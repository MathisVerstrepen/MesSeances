package accounts

import (
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"messeances/api/internal/accountmail"
)

func TestActionMailMessage(t *testing.T) {
	const recipient = "person@example.com"
	const token = "fixture-token_123-abc"
	for _, tt := range []struct {
		purpose TokenPurpose
		path    string
		title   string
		button  string
	}{
		{TokenVerification, "/verification", "Vérifiez votre adresse email", "Vérifier mon adresse email"},
		{TokenPasswordReset, "/reinitialiser-mot-de-passe", "Réinitialisez votre mot de passe", "Réinitialiser mon mot de passe"},
		{TokenEmailChange, "/compte/confirmer-email", "Confirmez votre nouvelle adresse", "Confirmer mon nouvel email"},
		{TokenEmailStepUp, "/compte/confirmer-identite", "Confirmez votre identité", "Confirmer mon identité"},
	} {
		t.Run(string(tt.purpose), func(t *testing.T) {
			link := "https://messeances.fr" + tt.path + "#token=" + token
			message, err := actionMailMessage("https://messeances.fr", recipient, link, tt.purpose)
			if err != nil {
				t.Fatal(err)
			}
			subject := "Confirmez votre demande MesSeances"
			instruction := "Pour confirmer votre demande, ouvrez ce lien puis validez le formulaire :"
			if tt.purpose == TokenEmailStepUp {
				subject = "Confirmez votre identité MesSeances"
				instruction = "Pour confirmer votre identité, ouvrez ce lien puis validez le formulaire :"
			}
			if message.Recipient != recipient || message.Subject != subject || message.Text != instruction+"\n"+link {
				t.Fatalf("non-HTML mail contract changed: %+v", message)
			}
			doc := inspectMailHTML(t, message.HTML, "https://messeances.fr/pwa-64x64.png")
			if len(doc.links) != 1 || doc.links[0] != link {
				t.Fatalf("action URL changed: %q", doc.links)
			}
			if len(doc.headings) != 1 || doc.headings[0] != tt.title {
				t.Fatalf("unexpected heading: %q", doc.headings)
			}
			if !strings.Contains(doc.text, instruction) || !strings.Contains(doc.text, tt.button) {
				t.Fatal("missing confirmation instruction or descriptive action label")
			}
			if strings.Count(message.HTML, token) != 1 || strings.Contains(doc.text, token) {
				t.Fatal("token must appear only in the action URL, not preview or visible text")
			}
		})
	}
}

func TestSecurityMailMessage(t *testing.T) {
	for _, text := range []string{
		"Votre mot de passe MesSeances a été réinitialisé.",
		"Le mot de passe de votre compte MesSeances a été modifié.",
		"Un changement d’adresse email a été demandé pour votre compte MesSeances.",
		"L’adresse email de votre compte MesSeances a été modifiée.",
		"Cette adresse email est maintenant associée à votre compte MesSeances.",
		"Une connexion Google a été ajoutée à votre compte MesSeances.",
		"La connexion Google a été retirée de votre compte MesSeances.",
		`Adresse <script>alert("unsafe")</script> & "nouvelle" 'adresse' <img src="https://example.com/pixel">`,
	} {
		t.Run(text, func(t *testing.T) {
			message, err := securityMailMessage("https://messeances.fr", "notice@example.com", text)
			if err != nil {
				t.Fatal(err)
			}
			if message.Recipient != "notice@example.com" || message.Subject != "Sécurité de votre compte MesSeances" || message.Text != text {
				t.Fatalf("non-HTML security mail contract changed: %+v", message)
			}
			doc := inspectMailHTML(t, message.HTML, "https://messeances.fr/pwa-64x64.png")
			if len(doc.links) != 0 {
				t.Fatalf("security notice unexpectedly contains action links: %q", doc.links)
			}
			if !strings.Contains(doc.text, text) {
				t.Fatal("notification text did not survive HTML escaping")
			}
		})
	}
}

func TestMailTemplateEscaping(t *testing.T) {
	const hostile = `<script>alert("unsafe")</script> & 'text' <img src="https://example.com/pixel">`
	const link = `https://messeances.fr/verification?value="><img src=x>&other=yes#token=test-token`
	message, err := renderMailMessage("https://messeances.fr", accountmail.Message{}, mailContent{
		Label: hostile, Title: hostile, Body: hostile, ActionLabel: hostile, ActionURL: link,
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := inspectMailHTML(t, message.HTML, "https://messeances.fr/pwa-64x64.png")
	// Title metadata, label, heading, body and action label must all remain text.
	if strings.Count(doc.text, hostile) != 5 {
		t.Fatal("dynamic text changed or was interpreted as markup")
	}
	if len(doc.links) != 1 {
		t.Fatalf("unexpected links: %q", doc.links)
	}
	decoded, err := url.QueryUnescape(doc.links[0])
	if err != nil || decoded != link {
		t.Fatalf("escaped URL changed meaning: %q, %v", doc.links[0], err)
	}
}

func TestActionMailURLSafety(t *testing.T) {
	for _, link := range []string{
		"https://messeances.fr/verification?one=1&two=2#token=abc_DEF-123",
		"http://localhost:3000/verification#token=abc_DEF-123",
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
	} {
		t.Run(link, func(t *testing.T) {
			message, err := actionMailMessage("https://messeances.fr", "person@example.com", link, TokenVerification)
			if err != nil {
				t.Fatal(err)
			}
			doc := inspectMailHTML(t, message.HTML, "https://messeances.fr/pwa-64x64.png")
			want := link
			if !strings.HasPrefix(link, "http") {
				want = "#ZgotmplZ"
			}
			if len(doc.links) != 1 || doc.links[0] != want {
				t.Fatalf("unexpected action URL: %q, want %q", doc.links, want)
			}
		})
	}
}

func TestMailIconUsesConfiguredOrigin(t *testing.T) {
	const recipient = "private-recipient@example.com"
	const token = "private-action-token"
	// Deliberately distinct from the configured origin: the icon must not be
	// derived from an action link or carry any of its recipient/token data.
	const link = "https://action.example/verification?email=" + recipient + "#token=" + token
	for _, origin := range []string{"https://messeances.fr", "https://preview.example:8443", "http://localhost:3000"} {
		for _, purpose := range []TokenPurpose{TokenVerification, TokenPasswordReset, TokenEmailChange, TokenEmailStepUp, "security_notification"} {
			t.Run(origin+"/"+string(purpose), func(t *testing.T) {
				var message accountmail.Message
				var err error
				if purpose == "security_notification" {
					message, err = securityMailMessage(origin, recipient, "Votre mot de passe a été modifié.")
				} else {
					message, err = actionMailMessage(origin, recipient, link, purpose)
				}
				if err != nil {
					t.Fatal(err)
				}
				doc := inspectMailHTML(t, message.HTML, origin+"/pwa-64x64.png")
				for _, image := range doc.images {
					parsed, err := url.Parse(image)
					if err != nil {
						t.Fatal(err)
					}
					if !parsed.IsAbs() || parsed.Path != "/pwa-64x64.png" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil || strings.Contains(image, recipient) || strings.Contains(image, token) {
						t.Fatalf("icon URL leaks private data or is not the static app icon: %q", image)
					}
				}
			})
		}
	}
}

func TestMailIconURLEscaping(t *testing.T) {
	// Production origins are validated configuration. These invalid origins
	// exercise the template's attribute escaping and scheme filter in isolation.
	for _, tt := range []struct {
		origin string
		want   string
	}{
		{`https://example.com/" onerror="alert(1)`, "https://example.com/%22%20onerror=%22alert%281%29/pwa-64x64.png"},
		{"https://example.com?one=1&two=2", "https://example.com?one=1&two=2/pwa-64x64.png"},
		{"javascript:alert(1)", "#ZgotmplZ"},
		{"data:image/png;base64,AAAA", "#ZgotmplZ"},
	} {
		t.Run(tt.origin, func(t *testing.T) {
			message, err := securityMailMessage(tt.origin, "person@example.com", "Avis de sécurité")
			if err != nil {
				t.Fatal(err)
			}
			inspectMailHTML(t, message.HTML, tt.want)
		})
	}
}

type mailHTMLInspection struct {
	links    []string
	images   []string
	headings []string
	text     string
}

func inspectMailHTML(t *testing.T, body, iconURL string) mailHTMLInspection {
	t.Helper()
	for _, marker := range []string{`<html lang="fr">`, "#f8f7f2", "#fcfaf8", "#27272a", "#991b1b", "#a8bfa3", "#ffcf3f", "Arial,Helvetica,sans-serif", "MesSeances"} {
		if !strings.Contains(body, marker) {
			t.Errorf("missing branded layout marker %q", marker)
		}
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var result mailHTMLInspection
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "html", "head", "meta", "title", "body", "table", "tbody", "tr", "td", "p", "span", "h1", "a", "img":
			default:
				t.Errorf("unexpected email element %q", node.Data)
			}
			presentation := false
			imageAttrs := make(map[string]string)
			for _, attr := range node.Attr {
				if node.Data == "img" {
					imageAttrs[attr.Key] = attr.Val
					switch attr.Key {
					case "src", "alt", "width", "height", "style":
					default:
						t.Errorf("unexpected image attribute %q", attr.Key)
					}
				}
				if attr.Key == "src" && (node.Data != "img" || attr.Val != iconURL) {
					t.Errorf("unexpected resource URL: %q", attr.Val)
				}
				if attr.Key == "href" {
					if node.Data != "a" {
						t.Errorf("URL outside action link: %s", node.Data)
					}
					result.links = append(result.links, attr.Val)
				}
				if strings.HasPrefix(attr.Key, "on") || attr.Key == "srcset" || attr.Key == "background" || attr.Key == "class" || strings.Contains(strings.ToLower(attr.Val), "url(") {
					t.Errorf("runtime dependency, resource or event handler: %s", attr.Key)
				}
				presentation = presentation || (attr.Key == "role" && attr.Val == "presentation")
			}
			if node.Data == "img" {
				result.images = append(result.images, imageAttrs["src"])
				if imageAttrs["src"] != iconURL || imageAttrs["alt"] != "Clap de cinéma" || imageAttrs["width"] != "40" || imageAttrs["height"] != "40" {
					t.Errorf("unexpected app icon or missing fallback dimensions: %v", imageAttrs)
				}
			}
			if node.Data == "table" && !presentation {
				t.Error("layout table lacks presentation role")
			}
			if node.Data == "h1" && node.FirstChild != nil {
				result.headings = append(result.headings, node.FirstChild.Data)
			}
		}
		if node.Type == html.TextNode {
			if strings.TrimSpace(node.Data) == "MS" {
				t.Error("MS placeholder remains")
			}
			result.text += node.Data
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	if len(result.images) != 1 {
		t.Errorf("expected one app icon, got %q", result.images)
	}
	if !strings.Contains(result.text, "MesSeances.") {
		t.Error("missing independent text wordmark when images are blocked")
	}
	return result
}
