package veo

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// CheckoutActionPathFacts contains only closed classes and bounded equality
// facts. It is backend instrumentation, never a booking acceptance predicate.
// Counts and token positions saturate at 1024; classes retain at most 32 items.
type CheckoutActionPathFacts struct {
	Parsed, CanonicalValid, Exact                         bool
	Absolute, SchemeEqual, AuthorityEqual                 bool
	QueryPresent, FragmentPresent, UserinfoPresent        bool
	EscapingPresent, LeadingSlash, TrailingSlash          bool
	CanonicalPrefix, ExpectedTokensInOrder                bool
	SegmentCount, EmptySegments, ExtraCount, MissingCount int
	Shape                                                 string
	Classes                                               []string
	ClassesTruncated, CountsTruncated                     bool
	ExpectedAtPosition                                    [6]bool
	ExpectedPositions                                     [6]int
	ExpectedPresentMask, ExpectedPositionMask             uint8
}

type diagnosticPathStructure = CheckoutActionPathFacts

var diagnosticFilmSegment = regexp.MustCompile(`^F[1-9][0-9]*$`)
var diagnosticEpochSegment = regexp.MustCompile(`^D[0-9]{10}$`)
var diagnosticDecimalSegment = regexp.MustCompile(`^[0-9]+$`)

func diagnosticPathSegments(path string) []string {
	if path == "" || path == "/" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/"), "/")
}

// Parsed URLs and errors (which can quote input) never leave this function.
func classifyDiagnosticActionPath(action, canonical string) diagnosticPathStructure {
	d := diagnosticPathStructure{Shape: "canonical_unavailable", ExpectedPositions: [6]int{-1, -1, -1, -1, -1, -1}}
	c, err := url.Parse(canonical)
	if err != nil || canonical == "" || !safeURL(c) || c.String() != canonical || c.Host != "achat.veocinemas.fr" || c.RawQuery != "" || !checkoutDestination.MatchString(c.Path) {
		return d
	}
	d.CanonicalValid = true
	u, err := url.Parse(action)
	if err != nil {
		d.Shape = "unparseable"
		return d
	}
	d.Parsed, d.Exact, d.Absolute = true, action == canonical, u.IsAbs()
	d.SchemeEqual, d.AuthorityEqual = u.Scheme == c.Scheme, u.Host == c.Host
	d.QueryPresent, d.FragmentPresent, d.UserinfoPresent = u.RawQuery != "" || u.ForceQuery, strings.Contains(action, "#"), u.User != nil
	d.EscapingPresent = strings.ContainsAny(action, `%\`)
	d.LeadingSlash, d.TrailingSlash = strings.HasPrefix(u.Path, "/"), strings.HasSuffix(u.Path, "/")
	parts, expected := diagnosticPathSegments(u.Path), diagnosticPathSegments(c.Path)
	d.SegmentCount = len(parts)
	if len(parts) > len(expected) {
		d.ExtraCount = len(parts) - len(expected)
	} else {
		d.MissingCount = len(expected) - len(parts)
	}
	labels := [6]string{"expected_slug", "route_reserver", "expected_film", "expected_epoch", "expected_version", "expected_tail"}
	for i, part := range parts {
		class := "other"
		switch {
		case part == "":
			d.EmptySegments++
			class = "empty"
		case part == "reserver":
			class = "route_reserver"
		case part == "r":
			class = "route_source_r"
		case diagnosticFilmSegment.MatchString(part):
			class = "film_like"
		case diagnosticEpochSegment.MatchString(part):
			class = "epoch_like"
		case part == "VF" || part == "VO":
			class = "version_enum"
		case diagnosticDecimalSegment.MatchString(part):
			class = "decimal"
		}
		for j, token := range expected {
			if part == token {
				class = labels[j]
				d.ExpectedPresentMask |= 1 << j
				if d.ExpectedPositions[j] == -1 {
					d.ExpectedPositions[j] = i
				}
				if i == j {
					d.ExpectedAtPosition[j] = true
					d.ExpectedPositionMask |= 1 << j
				}
			}
		}
		if i < 32 {
			d.Classes = append(d.Classes, class)
		}
	}
	d.ClassesTruncated = len(parts) > len(d.Classes)
	d.CanonicalPrefix = len(parts) >= len(expected)
	d.ExpectedTokensInOrder = true
	last := -1
	for i := range expected {
		d.CanonicalPrefix = d.CanonicalPrefix && d.ExpectedAtPosition[i]
		if d.ExpectedPositions[i] <= last {
			d.ExpectedTokensInOrder = false
		}
		last = d.ExpectedPositions[i]
	}
	switch {
	case slices.Equal(parts, expected):
		d.Shape = "canonical_tokens"
	case len(parts) > len(expected) && d.CanonicalPrefix:
		d.Shape = "canonical_prefix_extra"
	case len(parts) == 2 && parts[0] == expected[0] && parts[1] == "reserver":
		d.Shape = "generic_entry"
	case len(parts) == 3 && parts[0] == expected[0] && parts[1] == "r" || len(parts) == 4 && parts[0] == expected[0] && parts[1] == "reserver" && parts[2] == "r":
		d.Shape = "source_alias"
	case d.EmptySegments > 0:
		d.Shape = "empty_segments"
	case len(parts) < len(expected):
		d.Shape = "missing_segments"
	case allDiagnosticTokensPresent(d.ExpectedPositions) && !d.ExpectedTokensInOrder:
		d.Shape = "reordered_tokens"
	default:
		d.Shape = "different_shape"
	}
	for _, count := range []*int{&d.SegmentCount, &d.EmptySegments, &d.ExtraCount, &d.MissingCount, &d.ExpectedPositions[0], &d.ExpectedPositions[1], &d.ExpectedPositions[2], &d.ExpectedPositions[3], &d.ExpectedPositions[4], &d.ExpectedPositions[5]} {
		if *count > 1024 {
			*count = 1024
			d.CountsTruncated = true
		}
	}
	return d
}

func allDiagnosticTokensPresent(positions [6]int) bool {
	for _, p := range positions {
		if p < 0 {
			return false
		}
	}
	return true
}
