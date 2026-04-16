package models

import "time"

type CalendarDay struct {
	Year  int
	Month time.Month
	Day   int
}

type ActivitySnapshot struct {
	Cutoffs            []time.Time
	DayIndex           map[CalendarDay]int
	DailyActivity      []float64
	DailyTags          []float64
	FirstCommitByEmail map[string]time.Time
	OldestCommit       time.Time
	HasCommits         bool
}
