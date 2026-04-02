# publiccode.yml crawler voor developer.overheid.nl

## Beschrijving

Developer.overheid.nl biedt een catalogus van Open Source projecten van de
overheid.

`don-crawler` crawlt de repositories van deze open source projecten door een
lijst van publishers af te lopen. Hij zoekt daarin specifiek naar
`publiccode.yml` bestanden.

## Achtergrond

Dit project is ooit gestart als een fork van de Developers Italia
`publiccode-crawler`, maar is daarna als losse kopie doorontwikkeld en aangepast
voor developer.overheid.nl.

## Waarom publiccode.yml

We gebruiken de publiccode.yml-standaard om metadata over open source projecten
op een consistente en machine-leesbare manier vast te leggen. De standaard heeft
twee doelen: projecten vindbaar maken en projectinformatie centraliseren. Het
bestand hoort in de root van de repository.

Voordelen die dit oplevert:

- metadata staat in de codebase en is daarmee git-platform agnostisch;
- metadata is machine-leesbaar en kan automatisch door catalogi worden
  ingelezen;
- projecten zijn eenvoudiger te vinden (o.a. door bots die repos afstruinen op
  `publiccode.yml` in de root).

Meer uitleg staat op developer.overheid.nl in de toelichting bij de standaard:
https://developer.overheid.nl/kennisbank/open-source/standaarden/publiccode-yml

## Configuratie

Configuratie gaat via environment variables. Een lokaal `.env` bestand wordt
automatisch geladen als het aanwezig is.

De crawler gebruikt op dit moment de volgende variabelen:

| Variabele | Verplicht | Doel |
| --- | --- | --- |
| `API_BASEURL` | ja, voor API-calls | Basis-URL van de DON API. |
| `API_X_API_KEY` | ja, voor API-calls | Waarde voor de `x-api-key` header bij API-requests. |
| `KEYCLOAK_BASE_URL` | ja, voor API-auth | Basis-URL van Keycloak. |
| `KEYCLOAK_REALM` | ja, voor API-auth | Keycloak realm voor token-opvraag. |
| `AUTH_CLIENT_ID` | ja, voor API-auth | Client ID voor de Keycloak `client_credentials` flow. |
| `AUTH_CLIENT_SECRET` | ja, voor API-auth | Client secret voor de Keycloak `client_credentials` flow. |
| `GIT_OAUTH_CLIENTID` | ja, voor GitHub scanning | GitHub App ID. |
| `GIT_OAUTH_INSTALLATION_ID` | ja, voor GitHub scanning | GitHub App installation ID. |
| `GIT_OAUTH_SECRET` | ja, voor GitHub scanning | GitHub App private key in PEM-formaat. |
| `DATADIR` | nee | Directory voor lokale data en clones. Default: `/app/data`. |
| `ACTIVITY_DAYS` | nee | Aantal dagen voor activity/vitality-bepaling. Default: `60`. |

Opmerkingen:

- `GIT_OAUTH_SECRET` mag een PEM private key zijn met echte newlines of met
  escaped `\n`.
- Zonder Keycloak-variabelen kan de crawler geen bearer token ophalen voor
  authenticated API-requests.

## Build en run

```console
go build
```

## Gebruik

Op dit moment ondersteunen we alleen het `crawl` command.

```console
publiccode-crawler crawl
```

## Authors

De oorspronkelijke crawler is ontwikkeld door Developers Italia. Deze repository
wordt onderhouden als aparte, aangepaste kopie voor developer.overheid.nl.

## Changelog (Changie)

Voor user-facing wijzigingen (fix/feature/breaking) verwachten we per PR een Changie-fragment in `.changes/unreleased`.

Eenmalig installeren:

```bash
go install github.com/miniscruff/changie@latest
```

Fragment aanmaken:

```bash
changie new
```

Normaal is een fragment niet nodig voor interne refactors zonder zichtbaar effect, docs-only wijzigingen en CI-only tweaks.

Bij een release kun je de fragments bundelen in `CHANGELOG.md`:

```bash
changie batch <version>
```

Dit gebeurt ook automatisch bij elke merge naar `main` via GitHub Actions:
`changie batch auto` en daarna `changie merge`, waarna automatisch een PR met de changelog-updates wordt aangemaakt.

## Deployen

De deployment van deze site verloopt via GitHub Actions en een aparte infra
repository.

### Benodigde variabelen en secrets

- Organization variable `INFRA_REPO`, bijvoorbeeld
  `developer-overheid-nl/don-infra`.
- Repository variable `KUSTOMIZE_PATH`, met als basispad bijvoorbeeld
  `apps/api/overlays/`.
- Secrets `RELEASE_PROCES_APP_ID` en `RELEASE_PROCES_APP_PRIVATE_KEY` voor het
  aanpassen van de infra repository.

### Deploy naar test

De testdeploy draait via
`.github/workflows/deploy-test.yml`.

- De workflow draait op pushes naar branches behalve `main`.
- Alleen commits met `[deploy-test]` in de commit message worden echt gedeployed.
- Er wordt een image gebouwd en gepusht naar
  `ghcr.io/<owner>/<repo>` met tags `test` en de commit SHA.
- Daarna wordt in `INFRA_REPO` het bestand
  `${KUSTOMIZE_PATH}test/kustomization.yaml` bijgewerkt naar de nieuwe image
  tag en direct gecommit.

Voorbeeld commit message:

```text
feat: pas content aan [deploy-test]
```

### Deploy naar productie

De productiedeploy draait via
`.github/workflows/deploy-prod.yml`.

- De workflow draait bij een push naar `main`.
- Er wordt in `INFRA_REPO` een release branch aangemaakt.
- In `${KUSTOMIZE_PATH}prod/kustomization.yaml` wordt de image tag bijgewerkt
  naar de commit SHA van deze repository.
- Daarna wordt automatisch een pull request in de infra repository geopend.
- De productie-uitrol gebeurt door die pull request te mergen.

### Contributies en deploy

Een contribution of pull request leidt niet automatisch tot een deployment.

- Een pull request triggert wel CI, waaronder de build en JSON-validatie.
- De build in `.github/workflows/go-ci.yml` bouwt voor een pull request een
  Docker image als controle, maar pusht dat image niet naar GHCR en past de
  infra repository niet aan.
- Er is dus geen automatische preview-omgeving per pull request.
- Een testdeploy gebeurt pas na een push naar een branch in deze repository met
  `[deploy-test]` in de commit message.
- Die testdeploy gebruikt repository- en organization-variables en secrets om
  ook `INFRA_REPO` aan te passen. Daardoor is dit pad in de praktijk bedoeld
  voor maintainers of contributors met een branch in deze repository.