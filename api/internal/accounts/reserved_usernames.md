# Reserved usernames

`reserved_usernames.json` is the embedded, sorted union imported on 2026-09-22. The application and tests do not read the original Downloads files at runtime. Import requires JSON arrays of strings, trims and lowercases entries, deduplicates, and sorts. Exact matching is case-insensitive; there is no fuzzy matching or Unicode transliteration. Username syntax is checked separately: 3-30 ASCII characters, leading letter, then lowercase letters, digits or underscores.

| Source | Entries | Normalized unique | Entries outside username syntax | Source SHA-256 |
| --- | --- | --- | --- | --- |
| `reserved-usernames.json` | 602 | 602 | 28 | `892df73c798b8c66910be6aa7cc489e944237a99fd1debcef8cd8a8a2acb8572` |
| `reserved-usernames2.json` | 568 | 567 | 75 | `278f5da24930e867f17e412c0ebb3f6c07feafa92398fe8180b44ea550dfd8dc` |

Original operator-supplied sources were `/home/mathis/Downloads/reserved-usernames.json` and `/home/mathis/Downloads/reserved-usernames2.json`. Second source repeats `archive`. Their normalized union contains 829 terms. Invalid-handle terms are deliberately retained rather than silently discarded.

## Application reservations

The approved overlay contains these 42 terms:

```text
messeances mes_seances messeance mes_seance movieflow cinema cinemas film films
seance seances planning recherche statistiques compte comptes connexion
deconnexion inscription verification finaliser aide assistance contact support
equipe officiel officielle moderation moderateur moderateurs administrateur
administrateurs confidentialite mentions_legales conditions credits securite
signalement mot_de_passe reinitialiser changement_email
```

`contact` and `support` already occur in source union; overlay adds 40 terms. Final list contains 869 unique terms, including 90 outside allowed username syntax. Canonical compact JSON SHA-256 is `76699aebd2ceca3d36faeb80049a0e58e6e97676b7223485805de6eba6430401`. Tests pin canonical content, counts, normalization and representative restrictions.

Database `account_username_claims` separately enforces uniqueness against active and permanently reserved deleted-account usernames. No username is reserved before onboarding succeeds. Usernames cannot be changed or reclaimed through account APIs.
