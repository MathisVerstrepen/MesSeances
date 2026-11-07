# IMAX guide maintenance

The French public guide lives at `/articles/imax`. It is linked from the footer's
Articles section, the `/articles` index and `/sitemaps/articles.xml`.
The old `/guides` and `/guides/imax` URLs permanently redirect to their new paths.
Update the editorial review date and sitemap `lastmod` when changing its factual content. References and image credits
are grouped at the end of the article, without inline bibliography anchors.

Cinema links resolve verified catalog IDs to current API slugs. Unknown entries
fall back to `/cinemas?technologies=IMAX&q=…`. The directory reads the public
catalog, gives `technologies` precedence over currently bookable formats, and
never treats an equipment label as proof of today's IMAX screenings or of a
particular laser/film installation. The article remains HTTP 200 if the catalog
is unavailable; its directory displays that state explicitly.

The 26% and 67% figures are rounded rectangle-area comparisons at equal width
against 2.39:1. They do not compare physical screens or resolution. Centered
masks over scan images illustrate geometry rather than reproduce release
masters. The source scans have slightly different image bounds; the display
fits them into a 1.43 rectangle. The unmasked film-strip illustration preserves
the full scan, including its perforations. Do not label these simulated crops as official
compositions.

## Visual assets

Optimized WebP copies (800 and 1600 pixels wide) were made from user-supplied
files in `/home/mathis/Pictures/IMAX Scans`; originals were not modified:

| Web asset stem | Original filename |
| --- | --- |
| `dune` (Dune : Deuxième Partie) | `img20240322_16114833 2 2.jpg` |
| `project-hail-mary` | `Project Hail Mary IMAX film cell strip scan.jpg` |
| `odyssee-voilier` | `The Odyssey IMAX 70mm Scan 7.jpg` |
| `odyssee-pellicule` (800, 1600 and 2400 px) | `The Odyssey IMAX 70mm Scan 3.jpg` |

The library contained no scanner attribution or license notes. The guide names
the films and attributes image rights to their respective holders;
it does not claim a license, ownership, or an identified scan author.

## Verification

Use the canonical frontend unit, typecheck and lint selections in
`docs/testing.md`. Browser coverage is in `web/e2e/imax-guide.spec.ts`:

```sh
npm --prefix web run test:browser -- e2e/imax-guide.spec.ts
```

The fixture covers desktop/mobile interaction, keyboard ratio selection,
visible movie buttons below the preview, scene switching, projection types, catalog outage,
IMAX directory filtering, fallback links, metadata, sitemap and footer entry.

## Reading flow and venue specifications

Booking advice precedes a single spotlight on Pathé Odysseum in Montpellier.
A panoramic exterior photograph leads the borderless presentation, without a
sage background. The full list and map link to the existing IMAX-filtered
directory. The four formerly featured venue cards and their unused specification
and source data have been removed. Catalogue reads still resolve the Odysseum
link and provide a named IMAX search fallback when unavailable.

The spotlight groups its heading, explanation and booking action beneath the
panorama. A separate subsection explains multiplex Laser 4K / 1.90 presentations
and hemispherical IMAX Dome presentations without listing venues. References are
IMAX's 15 May 2024 French laser deployment announcement and Universcience's
21 January 2025 dome press release. Nationwide directory and map actions follow
these explanations. Montpellier mentions in the article and FAQ link to
`/ville/montpellier/cinemas`, separately from the cinema's own booking link.

Montpellier's 1.43 Laser and 70 mm references remain the IMAX location list and
CNC article. Specifications were checked on 7 November 2026.

The panorama was supplied as
`/home/mathis/Downloads/651a485f-e04e-45cd-8108-aeb27d6dc5f8.png`.
Local `pathe-odysseum-800.webp`, `pathe-odysseum-1600.webp` and
`pathe-odysseum-2103.webp` preserve its full 2103 × 512 composition.

The compact mobile contents stays below the measured site header. It closes
on section selection or Escape, restores focus on Escape, and follows the
reading position without rewriting the URL on manual scroll. The desktop
contents uses the same current-section state. Observers and scroll listeners
are removed when leaving the guide. Reduced motion keeps native anchor jumps.

The frame comparison names its 2.39 baseline and labels the images as a framing
simulation. The projection comparison puts support/definition and maximum ratio
first; optional technical details retain the CoLa/GT names and optical caveats.


## Comparison sidebar and posters

The comparison uses a right sidebar for ratio and movie choices on desktop.
Below 1180 px, controls stack beneath the preview. The explanation spans the full
width beneath both columns, avoiding empty sidebar space next to the caption. MesSeances' square borders, cream
surfaces and sage selection colors remain unchanged. Ratio choices run from
1.43 to 2.39; the initial choice remains 1.90.

On smaller screens, the ratio choices use compact labelled tabs and the film
choices form a horizontally scrollable strip with posters, titles and directors.
The mobile result puts its area gain beside the heading and keeps the Scope
baseline visible. The header's decorative framing figure is desktop-only.

Local 160 × 240 WebP posters were optimized from the existing public catalog's
TMDB images, so the widget never needs external image requests at runtime:

| Asset | Source |
| --- | --- |
| `dune-poster.webp` | `https://image.tmdb.org/t/p/w185/iRNbRAIGQQr5diGnjpwJFm0dgt4.jpg` |
| `project-hail-mary-poster.webp` | `https://image.tmdb.org/t/p/w185/wzyy8ZrsuHfAt4iz4iH3rT0tdoT.jpg` |
| `odyssee-voilier-poster.webp` | `https://image.tmdb.org/t/p/w185/kP8jJIkmX0vWEYiqQ9c5zU0dvcn.jpg` |

Movie titles and director names remain available in the selector. Poster images
are decorative because the adjacent button text identifies each movie.

Below the interactive comparison, `ImaxRatioDiagram.vue` shows three centred
frames at equal width. Frame heights are calculated from their ratios, and the
image-area gains use the same Scope baseline as the interactive comparison.

## Expanded projection details

Only the optional technical panel contains the projector photos and the deeper
explanations of light sources, image formation, resolution, aspect-ratio limits,
alignment and film transport. The main comparison remains concise. Each system
links to technical reading within its expanded panel.

The four photos were supplied by the user. No photographer credit or original
publication URL was supplied. Optimized local WebP copies preserve their framing:

| Asset | Supplied file |
| --- | --- |
| `projecteur-imax-laser.webp` | `/home/mathis/Downloads/3j1wbpiaqmhb1.jpg` |
| `projecteur-imax-gt.webp` | `/home/mathis/Downloads/image.webp` |
| `projecteur-imax-film.webp` | `/home/mathis/Downloads/107273715-1689780929290-IMG_2085.jpg` |
| `projecteur-imax-xenon.webp` | `/home/mathis/Downloads/IMAX-DH-Xenon-Front.jpg` |

The introduction also includes a user-supplied auditorium photograph from
`/home/mathis/Downloads/7utcbthcq2r81.jpg`. Local `salle-imax-640.webp` and
`salle-imax-1200.webp` preserve its full framing. The theatre is not identified,
so its location and projection specifications are not inferred from the photo.

Film mentions resolve catalogue results by TMDB identity, including ended films,
and use the current public movie slug. They fall back to a film search when a
matching record is unavailable. Keep `imaxGuideMovies` in sync with the titles
discussed in the guide. Scene selection remains a button action, with a separate
link to the selected film in the comparison caption.

The camera, DMR and 18K explanations are three independent native disclosures.
They distinguish capture, remastering and projected detail, with primary-source
reading links inside each disclosure. The camera photograph was supplied as
`/home/mathis/Downloads/Imax-silmmaking-in-depth.003.webp`. Local
`camera-imax-640.webp` and `camera-imax-1200.webp` preserve its framing. No camera
model, photographer or production is inferred from the photograph.

The sound disclosure includes the user-supplied `/home/mathis/Downloads/imax-speaker-dt.jpg`.
Local `enceinte-imax-640.webp` and `enceinte-imax-1200.webp` preserve its framing.
The caption identifies an IMAX speaker without inferring its model or location.

## Price reference

The booking checklist gives an approximate €13–26 budget for an IMAX 2D adult
full-price ticket including the IMAX supplement. It is an illustrative budget,
not a national minimum, maximum or measured average. Source examples in the
article bibliography are Roubaix (€8.50 + €4 = €12.50, municipal article dated
2 August 2025), Cannes (€13.50 + €6 = €19.50, AlloCiné) and La Villette
(€17.20 + €8 = €25.20, Orange). Listings were checked on 7 November 2026.
The rounded range does not cover every discount, subscription, 3D or special
presentation. Refresh these examples whenever the price guidance is reviewed.
