# IMAX guide maintenance

The French public guide lives at `/guides/imax`. It is linked from the footer's
Guides section and `/sitemaps/guides.xml`. Update the editorial review date and
sitemap `lastmod` when changing its factual content. References and image credits
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

Booking advice precedes the cinema selection. The article presents Montpellier
and four featured venues; the complete list and map link to the existing IMAX
filtered directory. It does not duplicate the full directory or its search UI.
Catalog reads still resolve public cinema links and report the registered count.

`imaxFeaturedCinemas` lists only sourced capabilities, with `sourceId` pointing
to `imaxVenueSources`. Lomme and Dole's official IMAX listings confirm Laser;
Cineum's own rooms page confirms Laser 4K; Apsys documents La Géode's Laser 4K
dome. No flat-screen ratio is inferred for those venues. Montpellier's 1.43
Laser and 70 mm references remain the IMAX location list and CNC article.
Specifications were checked on 7 November 2026. Provider pages were fetched
through the configured proxy file; no proxy details or raw responses are saved.

The compact mobile contents stays below the measured site header. It closes
on section selection or Escape, restores focus on Escape, and follows the
reading position without rewriting the URL on manual scroll. The desktop
contents uses the same current-section state. Observers and scroll listeners
are removed when leaving the guide. Reduced motion keeps native anchor jumps.

The frame comparison names its 2.39 baseline and labels the images as a framing
simulation. The projection comparison puts support/definition and maximum ratio
first; optional technical details retain the CoLa/GT names and optical caveats.
