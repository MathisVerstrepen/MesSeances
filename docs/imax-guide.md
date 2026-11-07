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
