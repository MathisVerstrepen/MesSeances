package ugc

import (
	"strings"

	"golang.org/x/net/html"
)

func walk(node *html.Node, fn func(*html.Node)) {
	fn(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walk(child, fn)
	}
}
func descendants(node *html.Node, predicate func(*html.Node) bool) []*html.Node {
	out := []*html.Node{}
	walk(node, func(n *html.Node) {
		if n != node && predicate(n) {
			out = append(out, n)
		}
	})
	return out
}
func attr(node *html.Node, name string) string {
	for _, a := range node.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}
func attrAny(node *html.Node, names ...string) string {
	for _, name := range names {
		if value := attr(node, name); value != "" {
			return value
		}
	}
	return ""
}
func hasClass(node *html.Node, class string) bool {
	for candidate := range strings.FieldsSeq(attr(node, "class")) {
		if candidate == class {
			return true
		}
	}
	return false
}
func hasAncestorID(node *html.Node, id string) bool {
	for parent := node.Parent; parent != nil; parent = parent.Parent {
		if attr(parent, "id") == id {
			return true
		}
	}
	return false
}
func text(node *html.Node) string {
	var builder strings.Builder
	var appendVisibleText func(*html.Node)
	appendVisibleText = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "template":
				return
			}
		}
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
			builder.WriteByte(' ')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			appendVisibleText(child)
		}
	}
	appendVisibleText(node)
	return builder.String()
}
func collapse(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for field := range strings.FieldsSeq(value) {
		if result.Len() > 0 {
			result.WriteByte(' ')
		}
		result.WriteString(field)
	}
	return result.String()
}
func singleEqual(values []string, want string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if value != want {
			return false
		}
	}
	return true
}
func singleNonempty(values []string) (string, bool) {
	seen := ""
	for _, value := range values {
		value = collapse(value)
		if value == "" {
			continue
		}
		if seen != "" && seen != value {
			return "", false
		}
		seen = value
	}
	return seen, seen != ""
}
func hasEmptyScheduleMarker(root *html.Node) bool {
	found := false
	walk(root, func(n *html.Node) {
		explicitMarker := hasClass(n, "no-showing") || hasClass(n, "no-result")
		inShowingsContainer := attr(n, "id") == "showings" || hasAncestorID(n, "showings")
		if n.Type == html.ElementNode && explicitMarker && inShowingsContainer {
			found = true
		}
	})
	return found
}
