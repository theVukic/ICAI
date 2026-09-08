# ASI release i ažuriranja

Korisniku se šalje samo `ASI-Setup.exe`. Setup preuzima odabrane modele u
pozadini, provjerava SHA-256 i zatim instalira ASI bez potrebe da korisnik
ručno preuzima ZIP fajlove.

Tok setupa je: izbor foldera za aplikaciju, broj OLX korisnika, ime i token za
svakog korisnika, provjera svakog tokena preko OLX `/me` API-ja, pa unos
peppera. `token.json` i `pepper.json` se čuvaju u
`%LOCALAPPDATA%\ASIapp\backend`, nezavisno od foldera u koji je instaliran ASI.

## Objavljivanje verzije

Iz korijena projekta pokreni, na primjer:

```powershell
.\release.ps1 -Version 1.2.0
```

Skripta te redom pita za Full ili Custom paket, dodatne fajlove/foldere i
poruku za korisnike. Za folder ekstenzije odaberi `D`, izaberi folder, pa kao
cilj upiši `extensions`. Taj sadržaj postaje dio instalera i zasebna update
komponenta. Nakon uspješnog builda skripta pravi GitHub release, `v1.2.0` tag
i `latest.json` manifest.

Primjeri:

```powershell
.\release.ps1 -Version 1.2.0 -ReleaseNotes 'Ispravljen login i dodana nova ekstenzija'
.\release.ps1 -Version 1.2.0 -ReleaseNotesFile .\notes.txt
.\release.ps1 -Version 1.2.0 -DryRun
```

`-DryRun` napravi lokalni build/setup, ali ne objavljuje GitHub release ni tag.
Prvi pravi release traži GitHub prijavu i automatski instalira Inno Setup ako
nije pronađen.

## Tok ažuriranja

Pri svakom novom pokretanju ASI provjerava `latest.json`. Ako postoji novija
verzija, korisnik dobija modal sa `Ne` i `Da, ažuriraj`. `Ne` sakrije modal samo
u tom pokretanju. `Da` zatvara ASI prozor i backend, pokreće zasebni updater,
preuzima samo komponente čiji se SHA-256 promijenio, pa ponovno otvara ASI.
Nakon toga ASI pokaže verziju i release poruku koju si unio pri objavljivanju.
