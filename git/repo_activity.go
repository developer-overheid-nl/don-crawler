package git

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/developer-overheid-nl/don-crawler/common"
	"github.com/developer-overheid-nl/don-crawler/models"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	yaml "gopkg.in/yaml.v2"
)

// RangesData contains the data loaded from vitality-ranges.yml.
type RangesData []Ranges

// Ranges are the ranges for a specific parameter (userCommunity, codeActivity, releaseHistory, longevity).
type Ranges struct {
	Name   string
	Ranges []Range
}

// Range is a range between will be assigned Points value.
type Range struct {
	Min    float64
	Max    float64
	Points float64
}

// CalculateRepoActivity return the repository activity index and the vitality slice calculated on the git clone.
// It follows the document https://lg-acquisizione-e-riuso-software-per-la-pa.readthedocs.io/
// In reference to section: 2.5.2. Fase 2.2: Valutazione soluzioni riusabili per la PA.
func CalculateRepoActivity(repository common.Repository, days int) (float64, map[int]float64, error) {
	if repository.Name == "" {
		return 0, nil, errors.New("cannot  calculate repository activity without name")
	}

	if days < 1 {
		return 0, nil, errors.New("activity days must be at least 1")
	}

	vendor, repo := common.SplitFullName(repository.Name)
	path := filepath.Join(viper.GetString("DATADIR"), "repos", repository.URL.Host, vendor, repo, "gitClone")

	if _, err := os.Stat(path); err != nil {
		return 0, nil, err
	}

	r, err := git.PlainOpen(path)
	if err != nil {
		log.Error(err)

		return 0, nil, err
	}

	now := time.Now()
	activity, err := collectActivitySnapshot(r, days, now)
	if err != nil {
		log.Error(err)

		return 0, nil, err
	}

	if err := collectTagStats(r, activity); err != nil {
		log.Error(err)
	}

	longevity, err := activityLongevity(activity)
	if err != nil {
		log.Warn(err)
	}

	rangeData, err := loadRangesData()
	if err != nil {
		log.Error(err)
	}

	vitalityIndex := make(map[int]float64, days)
	var total float64

	for i := range days {
		userCommunity := rangePoints(rangeData, "userCommunity", userCommunityBefore(activity, i))
		codeActivity := rangePoints(rangeData, "codeActivity", activity.DailyActivity[i])
		releaseHistory := rangePoints(rangeData, "releaseHistory", activity.DailyTags[i])

		repoActivity := userCommunity + codeActivity + releaseHistory + rangePoints(rangeData, "longevity", longevity)
		if repoActivity > 100 {
			repoActivity = 100
		}

		vitalityIndex[i] = repoActivity
		total += repoActivity
	}

	vitalityIndexTotal := total / float64(len(vitalityIndex))
	if vitalityIndexTotal > 100 {
		vitalityIndexTotal = 100
	}

	return float64(int(vitalityIndexTotal)), vitalityIndex, nil
}

func collectActivitySnapshot(r *git.Repository, days int, now time.Time) (*models.ActivitySnapshot, error) {
	ref, err := r.Head()
	if err != nil {
		return nil, err
	}

	cIter, err := r.Log(&git.LogOptions{From: ref.Hash()})
	if err != nil {
		return nil, err
	}
	defer cIter.Close()

	activity := newActivitySnapshot(days, now)

	if err := cIter.ForEach(func(c *object.Commit) error {
		addCommitToActivity(activity, c)

		return nil
	}); err != nil {
		return nil, err
	}

	return activity, nil
}

func collectTagStats(r *git.Repository, activity *models.ActivitySnapshot) error {
	tagrefs, err := r.Tags()
	if err != nil {
		return err
	}
	defer tagrefs.Close()

	return tagrefs.ForEach(func(t *plumbing.Reference) error {
		if t.Hash().IsZero() {
			return nil
		}

		tagObject, err := r.CommitObject(t.Hash())
		if err != nil {
			return nil
		}

		addTagCommitToActivity(activity, tagObject)

		return nil
	})
}

func newActivitySnapshot(days int, now time.Time) *models.ActivitySnapshot {
	activity := &models.ActivitySnapshot{
		Cutoffs:            make([]time.Time, days),
		DayIndex:           make(map[models.CalendarDay]int, days),
		DailyActivity:      make([]float64, days),
		DailyTags:          make([]float64, days),
		FirstCommitByEmail: make(map[string]time.Time),
	}

	for i := range days {
		dayTime := now.AddDate(0, 0, -i)
		activity.Cutoffs[i] = dayTime
		activity.DayIndex[calendarDayFromTime(dayTime)] = i
	}

	return activity
}

func addCommitToActivity(activity *models.ActivitySnapshot, c *object.Commit) {
	if c == nil {
		return
	}

	commitTime := c.Author.When

	if !activity.HasCommits || commitTime.Before(activity.OldestCommit) {
		activity.OldestCommit = commitTime
		activity.HasCommits = true
	}

	if email := c.Author.Email; email != "" {
		if firstCommit, ok := activity.FirstCommitByEmail[email]; !ok || commitTime.Before(firstCommit) {
			activity.FirstCommitByEmail[email] = commitTime
		}
	}

	if idx, ok := activity.DayIndex[calendarDayFromTime(commitTime)]; ok {
		activity.DailyActivity[idx]++
		if c.NumParents() > 1 {
			activity.DailyActivity[idx]++
		}
	}
}

func addTagCommitToActivity(activity *models.ActivitySnapshot, c *object.Commit) {
	if c == nil {
		return
	}

	if idx, ok := activity.DayIndex[calendarDayFromTime(c.Author.When)]; ok {
		activity.DailyTags[idx]++
	}
}

func userCommunityBefore(activity *models.ActivitySnapshot, day int) float64 {
	cutoff := activity.Cutoffs[day]
	count := 0

	for _, firstCommit := range activity.FirstCommitByEmail {
		if firstCommit.Before(cutoff) {
			count++
		}
	}

	return float64(count)
}

func activityLongevity(activity *models.ActivitySnapshot) (float64, error) {
	if !activity.HasCommits {
		return 0, errors.New("no commits found")
	}

	age := time.Since(activity.OldestCommit).Hours() / 24

	then := time.Date(2005, time.January, 1, 1, 0, 0, 0, time.UTC)
	if age > time.Since(then).Hours()/24 {
		return -1, errors.New("first commit is too old. Must be after the creation of git (2005)")
	}

	return age, nil
}

func calendarDayFromTime(t time.Time) models.CalendarDay {
	year, month, day := t.Date()

	return models.CalendarDay{
		Year:  year,
		Month: month,
		Day:   day,
	}
}

func loadRangesData() (RangesData, error) {
	data, err := os.ReadFile("vitality-ranges.yml")
	if err != nil {
		return nil, err
	}

	var parsed RangesData
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}

	return parsed, nil
}

func rangePoints(data RangesData, name string, value float64) float64 {
	for _, v := range data {
		if v.Name != name {
			continue
		}

		for _, r := range v.Ranges {
			if value >= r.Min && value < r.Max {
				return r.Points
			}
		}
	}

	return 0
}

// meanActivity return the mean of all the points.
func meanActivity(points map[int]float64) float64 {
	if len(points) == 0 {
		return 0
	}

	var total float64
	for _, point := range points {
		total += point
	}

	return total / float64(len(points))
}

// LastCommitTime returns the commit time of HEAD.
func LastCommitTime(repository common.Repository) (time.Time, error) {
	if repository.Name == "" {
		return time.Time{}, errors.New("cannot determine last activity without repository name")
	}

	vendor, repo := common.SplitFullName(repository.Name)
	path := filepath.Join(viper.GetString("DATADIR"), "repos", repository.URL.Host, vendor, repo, "gitClone")

	if _, err := os.Stat(path); err != nil {
		return time.Time{}, err
	}

	r, err := git.PlainOpen(path)
	if err != nil {
		return time.Time{}, err
	}

	ref, err := r.Head()
	if err != nil {
		return time.Time{}, err
	}

	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return time.Time{}, err
	}

	return commit.Author.When, nil
}
