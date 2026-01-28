# publiccode.yml crawler voor developer.overheid.nl

## Beschrijving

developer.overheid.nl biedt een catalogus van Free and Open Source software
voor publieke organisaties.

`don-crawler` crawlt repositories van publishers uit de developer.overheid.nl
bronnen en zoekt daarin specifiek naar `publiccode.yml` bestanden.

## Achtergrond

Dit project is ooit gestart als een fork van de Developers Italia
`publiccode-crawler`, maar is daarna als losse kopie doorontwikkeld en aangepast
voor developer.overheid.nl.

## Waarom publiccode.yml

We gebruiken de publiccode.yml-standaard om metadata over open source projecten
op een consistente en machine-leesbare manier vast te leggen. De standaard heeft
twee doelen: projecten vindbaar maken en projectinformatie centraliseren.
Het bestand hoort in de root van de repository.

Voordelen die dit oplevert:

- metadata staat in de codebase en is daarmee git-platform agnostisch;
- metadata is machine-leesbaar en kan automatisch door catalogi worden ingelezen;
- projecten zijn eenvoudiger te vinden (o.a. door bots die repos afstruinen op
  `publiccode.yml` in de root).

Meer uitleg staat op developer.overheid.nl in de toelichting bij de standaard:
https://developer.overheid.nl/kennisbank/open-source/standaarden/publiccode-yml

## Configuratie

Configuratie gaat via environment variables (optioneel uit een `.env` bestand).

Belangrijkste variabelen:

- `API_BASEURL` (basis-URL van de API)
- `API_X_API_KEY` (optioneel, indien nodig)
- `GIT_OAUTH_CLIENTID`, `GIT_OAUTH_INSTALLATION_ID`, `GIT_OAUTH_SECRET` (GitHub App)
- `GITLAB_TOKEN` (optioneel, voor GitLab)
- `DATADIR` (default `./data`)
- `ACTIVITY_DAYS` (default `60`)
- `LOG_FILE` (optioneel)

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
