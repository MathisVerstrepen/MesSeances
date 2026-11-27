package accounts

import (
	"bytes"
	"html/template"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"messeances/api/internal/accountmail"
)

const digestPosterLimit = 4

type digestFilm struct {
	Title, URL, Poster, Initials string
}

// digestItem is one inbox entry laid out film-first: a single film gets a
// poster card, several films a poster row.
type digestItem struct {
	Tag, Title, Detail, ActionLabel, ActionURL string
	Featured                                   *digestFilm
	Films                                      []digestFilm
	MoreFilms                                  int
}

type digestContent struct {
	Test                                                                                   bool
	Title, Intro, Preheader, More, IconURL, InboxURL, SettingsURL, UnsubscribeURL, Cadence string
	Items                                                                                  []digestItem
}

var tmdbPosterSize = regexp.MustCompile(`^/t/p/[a-z0-9]+/`)

// emailPoster keeps only HTTPS poster URLs and asks TMDB for a mail-sized
// rendition; provider CDN posters are used unchanged.
func emailPoster(raw *string) string {
	if raw == nil || len(*raw) > 2048 {
		return ""
	}
	u, err := url.Parse(*raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	if u.Host == "image.tmdb.org" && tmdbPosterSize.MatchString(u.Path) {
		u.Path = tmdbPosterSize.ReplaceAllString(u.Path, "/t/p/w185/")
	}
	return u.String()
}

// initials labels a missing poster: first letters of the first two words, or
// the first two letters of a single-word title.
func initials(title string) string {
	letter := func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r > 127
	}
	var out []rune
	words := strings.Fields(title)
	for _, word := range words {
		for _, r := range word {
			if letter(r) {
				out = append(out, r)
				break
			}
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) < 2 && len(words) > 0 {
		out = nil
		for _, r := range words[0] {
			if letter(r) && len(out) < 2 {
				out = append(out, r)
			}
		}
	}
	return strings.ToUpper(string(out))
}

func theaterPhrase(item NotificationView) string {
	if len(item.Theaters) == 0 {
		return strconv.Itoa(item.TheaterCount) + " " + plural(item.TheaterCount, "cinéma", "cinémas")
	}
	phrase := item.Theaters[0].Name
	if others := item.TheaterCount - 1; others > 0 {
		phrase += " et " + strconv.Itoa(others) + " " + plural(others, "autre cinéma", "autres cinémas")
	}
	return phrase
}

func digestLayout(origin string, item NotificationView) digestItem {
	films := make([]digestFilm, 0, len(item.Movies))
	for _, m := range item.Movies {
		films = append(films, digestFilm{Title: m.Title, URL: origin + "/film/" + url.PathEscape(m.Slug), Poster: emailPoster(m.PosterURL), Initials: initials(m.Title)})
	}
	where := theaterPhrase(item)
	if item.Kind == NotificationKindWatchlist {
		layout := digestItem{Tag: "De votre watchlist", Title: "Un film de votre watchlist", Detail: "Première séance à " + where, ActionLabel: "Voir les séances", ActionURL: origin + "/compte/watchlist"}
		if len(films) > 0 {
			layout.Featured, layout.Title, layout.ActionURL = &films[0], films[0].Title, films[0].URL
		}
		return layout
	}
	if item.MovieCount == 1 && len(films) == 1 {
		return digestItem{Tag: "Nouveau à l’affiche", Title: films[0].Title, Detail: "Arrive à " + where, ActionLabel: "Voir les séances", ActionURL: films[0].URL, Featured: &films[0]}
	}
	if len(films) > digestPosterLimit {
		films = films[:digestPosterLimit]
	}
	return digestItem{
		Tag:         "Nouveautés dans vos cinémas",
		Title:       strconv.Itoa(item.MovieCount) + " " + plural(item.MovieCount, "nouveau film", "nouveaux films"),
		Detail:      "À " + where,
		ActionLabel: "Voir l’activité",
		ActionURL:   origin + "/compte/activite",
		Films:       films,
		MoreFilms:   max(0, item.MovieCount-len(films)),
	}
}

// digestSubject names films first: inbox lists show the subject, not posters.
func digestSubject(items []digestItem, total int) string {
	seen := map[string]bool{}
	titles := []string{}
	more := total > len(items)
	for _, item := range items {
		names := []string{}
		if item.Featured != nil {
			names = append(names, item.Featured.Title)
		}
		for _, f := range item.Films {
			names = append(names, f.Title)
		}
		if item.MoreFilms > 0 {
			more = true
		}
		for _, name := range names {
			if !seen[name] {
				seen[name] = true
				titles = append(titles, name)
			}
		}
	}
	if len(titles) == 0 {
		return "Vos alertes MesSeances : " + strconv.Itoa(total) + " " + plural(total, "notification", "notifications")
	}
	if len(titles) > 2 {
		titles, more = titles[:2], true
	}
	subject := strings.Join(titles, ", ")
	if more {
		return subject + " et plus encore"
	}
	if len(titles) == 2 {
		subject = titles[0] + " et " + titles[1]
	}
	return subject + " · MesSeances"
}

// testDigestMailMessage previews the digest with the account's latest inbox
// entries (read or not). It is user-requested, so it carries no unsubscribe
// headers and its footer says why it was sent.
func testDigestMailMessage(origin, recipient string, items []NotificationView) (accountmail.Message, error) {
	message, err := renderDigestMail(origin, recipient, "", items, len(items), "", "", true)
	if err != nil {
		return message, err
	}
	message.Subject = "E-mail de test · MesSeances"
	return message, nil
}

func digestMailMessage(origin, recipient, frequency string, items []NotificationView, total int, oneClickURL, unsubscribePageURL string) (accountmail.Message, error) {
	return renderDigestMail(origin, recipient, frequency, items, total, oneClickURL, unsubscribePageURL, false)
}

func renderDigestMail(origin, recipient, frequency string, items []NotificationView, total int, oneClickURL, unsubscribePageURL string, test bool) (accountmail.Message, error) {
	cadence := "quotidien"
	if frequency == DigestWeekly {
		cadence = "hebdomadaire"
	}
	content := digestContent{
		Test:           test,
		Title:          "Vos alertes",
		Intro:          strconv.Itoa(total) + " " + plural(total, "notification non lue", "notifications non lues") + " depuis votre dernier résumé.",
		IconURL:        origin + "/pwa-64x64.png",
		InboxURL:       origin + "/compte/notifications",
		SettingsURL:    origin + "/compte/notifications?onglet=alertes",
		UnsubscribeURL: unsubscribePageURL,
		Cadence:        cadence,
	}
	if test {
		content.Title = "Les e-mails arrivent bien"
		content.Intro = "Vos prochaines notifications apparaîtront ici dès qu’une alerte se déclenchera."
		if len(items) > 0 {
			content.Intro = "Voici un aperçu de votre résumé avec vos dernières notifications."
		}
	}
	var text strings.Builder
	if test {
		text.WriteString(content.Title + "\n\n")
	}
	text.WriteString(content.Intro + "\n\n")
	for _, item := range items {
		layout := digestLayout(origin, item)
		content.Items = append(content.Items, layout)
		text.WriteString("- " + layout.Title + " — " + layout.Detail + "\n")
		for _, film := range layout.Films {
			text.WriteString("    · " + film.Title + " " + film.URL + "\n")
		}
		if layout.MoreFilms > 0 {
			text.WriteString("    · et " + strconv.Itoa(layout.MoreFilms) + " " + plural(layout.MoreFilms, "autre", "autres") + "\n")
		}
		text.WriteString("  " + layout.ActionURL + "\n")
	}
	if total > len(items) {
		content.More = "Et " + strconv.Itoa(total-len(items)) + " de plus dans votre boîte de notifications."
		text.WriteString("\n" + content.More + "\n")
	}
	content.Preheader = content.Intro
	if len(content.Items) > 0 && !test {
		content.Preheader = content.Items[0].Title + " — " + content.Items[0].Detail
	}
	text.WriteString("\nToutes vos notifications : " + content.InboxURL + "\n")
	text.WriteString("Gérer vos alertes : " + content.SettingsURL + "\n")
	if test {
		text.WriteString("\nVous recevez cet e-mail car vous avez demandé un test depuis votre compte.\n")
	} else {
		text.WriteString("\nVous recevez ce résumé " + cadence + " car vous l’avez activé. Se désabonner : " + unsubscribePageURL + "\n")
	}
	var body bytes.Buffer
	if err := digestMailTemplate.Execute(&body, content); err != nil {
		return accountmail.Message{}, err
	}
	return accountmail.Message{Recipient: recipient, Subject: digestSubject(content.Items, total), Text: text.String(), HTML: body.String(), UnsubscribeURL: oneClickURL}, nil
}

// Same visual frame as account mail, built from tables with inline styles for
// mail clients. Every dynamic value, including URLs, is escaped by
// html/template. Posters always carry dimensions and alt text, so blocked
// images degrade to labelled boxes.
var digestMailTemplate = template.Must(template.New("digest-mail").Funcs(template.FuncMap{
	"posterCell": posterCell,
	// Pad short poster rows so every poster keeps the same column width.
	"blankCells": func(n int) []struct{} { return make([]struct{}, max(0, digestPosterLimit-n)) },
}).Parse(`{{define "poster"}}<a href="{{.URL}}" style="text-decoration:none;color:#27272a;">{{if .Poster}}<img src="{{.Poster}}" alt="Affiche : {{.Title}}" width="{{.Width}}" height="{{.Height}}" style="display:block;width:100%;max-width:{{.Width}}px;height:auto;border:2px solid #27272a;background-color:#27272a;color:#ffffff;font-family:Arial,Helvetica,sans-serif;font-size:12px;">{{else}}<table role="presentation" width="{{.Width}}" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:{{.Width}}px;border-collapse:collapse;"><tr><td height="{{.Height}}" align="center" valign="middle" bgcolor="#27272a" style="height:{{.Height}}px;border:2px solid #27272a;color:#ffcf3f;font-family:Arial,Helvetica,sans-serif;font-size:28px;font-weight:900;letter-spacing:-1px;">{{.Initials}}</td></tr></table>{{end}}</a>{{end}}<!doctype html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
</head>
<body style="margin:0;padding:0;background-color:#f8f7f2;color:#27272a;font-family:Arial,Helvetica,sans-serif;-webkit-text-size-adjust:100%;">
  {{if .Preheader}}<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:#f8f7f2;font-size:1px;line-height:1px;">{{.Preheader}}</div>{{end}}
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="#f8f7f2" style="width:100%;border-collapse:collapse;">
    <tr>
      <td align="center" style="padding:32px 12px;">
        <!--[if mso]><table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0"><tr><td><![endif]-->
        <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:600px;border:2px solid #27272a;border-collapse:collapse;">
          <tr>
            <td bgcolor="#fcfaf8" style="padding:24px;border-bottom:2px solid #27272a;">
              <table role="presentation" cellpadding="0" cellspacing="0" border="0" style="border-collapse:collapse;">
                <tr>
                  <td width="40" height="40" align="center" bgcolor="#ffcf3f" style="width:40px;height:40px;">
                    <img src="{{.IconURL}}" alt="Clap de cinéma" width="40" height="40" style="display:block;width:40px;height:40px;border:0;color:#27272a;font-family:Arial,Helvetica,sans-serif;font-size:10px;">
                  </td>
                  <td style="padding-left:12px;color:#27272a;font-family:Arial,Helvetica,sans-serif;font-size:23px;font-weight:900;letter-spacing:-1px;">MesSeances<span style="color:#991b1b;">.</span></td>
                </tr>
              </table>
            </td>
          </tr>
          <tr>
            <td bgcolor="#ffffff" style="padding:32px 24px 8px;color:#27272a;font-family:Arial,Helvetica,sans-serif;">
              <p style="margin:0 0 20px;font-family:'Courier New',Courier,monospace;font-size:12px;line-height:20px;font-weight:bold;letter-spacing:1px;text-transform:uppercase;"><span style="background-color:#a8bfa3;color:#27272a;padding:4px 8px;">{{if .Test}}E-mail de test{{else}}Résumé {{.Cadence}}{{end}}</span></p>
              <h1 style="margin:0 0 12px;font-size:32px;line-height:38px;font-weight:900;letter-spacing:-1px;">{{.Title}}</h1>
              <p style="margin:0;font-size:16px;line-height:26px;">{{.Intro}}</p>
            </td>
          </tr>
          {{range .Items}}
          <tr>
            <td bgcolor="#ffffff" style="padding:16px 24px;">
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;border-collapse:collapse;border-top:2px solid #27272a;">
                <tr>
                  <td style="padding-top:18px;">
                    {{if .Featured}}
                    <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;border-collapse:collapse;">
                      <tr>
                        <td width="112" valign="top" style="width:112px;padding-right:18px;">
                          {{template "poster" (posterCell .Featured 112 168)}}
                        </td>
                        <td valign="top" style="font-family:Arial,Helvetica,sans-serif;color:#27272a;">
                          <p style="margin:0 0 10px;font-family:'Courier New',Courier,monospace;font-size:11px;line-height:16px;font-weight:bold;letter-spacing:1px;text-transform:uppercase;"><span style="background-color:#ffcf3f;color:#27272a;padding:3px 6px;">{{.Tag}}</span></p>
                          <p style="margin:0 0 8px;font-size:22px;line-height:27px;font-weight:900;letter-spacing:-0.5px;overflow-wrap:anywhere;"><a href="{{.ActionURL}}" style="color:#27272a;text-decoration:none;">{{.Title}}</a></p>
                          <p style="margin:0 0 16px;font-size:15px;line-height:22px;color:#3f3f46;">{{.Detail}}</p>
                          <a href="{{.ActionURL}}" style="display:inline-block;padding:9px 14px;border:2px solid #27272a;color:#27272a;background-color:#ffffff;font-size:14px;line-height:20px;font-weight:bold;text-decoration:none;">{{.ActionLabel}} &rarr;</a>
                        </td>
                      </tr>
                    </table>
                    {{else}}
                    <p style="margin:0 0 10px;font-family:'Courier New',Courier,monospace;font-size:11px;line-height:16px;font-weight:bold;letter-spacing:1px;text-transform:uppercase;"><span style="background-color:#a8bfa3;color:#27272a;padding:3px 6px;">{{.Tag}}</span></p>
                    <p style="margin:0 0 4px;font-size:22px;line-height:27px;font-weight:900;letter-spacing:-0.5px;color:#27272a;">{{.Title}}</p>
                    <p style="margin:0 0 16px;font-size:15px;line-height:22px;color:#3f3f46;">{{.Detail}}</p>
                    <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;border-collapse:collapse;table-layout:fixed;">
                      <tr>
                        {{range .Films}}
                        <td width="25%" valign="top" style="width:25%;padding:0 6px 0 0;">
                          {{template "poster" (posterCell . 124 186)}}
                          <p style="margin:6px 0 0;font-size:12px;line-height:16px;font-weight:bold;overflow-wrap:anywhere;"><a href="{{.URL}}" style="color:#27272a;text-decoration:none;">{{.Title}}</a></p>
                        </td>
                        {{end}}
                        {{range (blankCells (len .Films))}}<td width="25%" style="width:25%;">&nbsp;</td>{{end}}
                      </tr>
                    </table>
                    <p style="margin:14px 0 0;font-size:14px;line-height:20px;">{{if .MoreFilms}}<span style="color:#3f3f46;">Et {{.MoreFilms}} de plus · </span>{{end}}<a href="{{.ActionURL}}" style="color:#991b1b;font-weight:bold;text-decoration:underline;">{{.ActionLabel}} &rarr;</a></p>
                    {{end}}
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          {{end}}
          <tr>
            <td bgcolor="#ffffff" style="padding:8px 24px 32px;color:#27272a;font-family:Arial,Helvetica,sans-serif;">
              {{if .More}}<p style="margin:0 0 20px;padding-top:16px;border-top:2px solid #27272a;font-size:14px;line-height:22px;">{{.More}}</p>{{end}}
              <table role="presentation" cellpadding="0" cellspacing="0" border="0" style="border-collapse:collapse;">
                <tr>
                  <td align="center" bgcolor="#991b1b" style="border:2px solid #27272a;mso-padding-alt:14px 20px;">
                    <a href="{{.InboxURL}}" style="display:inline-block;padding:14px 20px;color:#ffffff;background-color:#991b1b;font-family:Arial,Helvetica,sans-serif;font-size:15px;line-height:22px;font-weight:bold;text-decoration:underline;">Voir toutes mes notifications</a>
                  </td>
                </tr>
              </table>
            </td>
          </tr>
          <tr>
            <td bgcolor="#fcfaf8" style="padding:20px 24px;border-top:2px solid #27272a;color:#27272a;font-family:Arial,Helvetica,sans-serif;font-size:13px;line-height:20px;">
              {{if .Test}}Vous recevez cet e-mail car vous avez demandé un test depuis votre compte.
              <a href="{{.SettingsURL}}" style="color:#27272a;">Gérer mes alertes</a>{{else}}Vous recevez ce résumé {{.Cadence}} car vous l’avez activé.
              <a href="{{.SettingsURL}}" style="color:#27272a;">Gérer mes alertes</a> ·
              <a href="{{.UnsubscribeURL}}" style="color:#27272a;">Se désabonner</a>{{end}}
            </td>
          </tr>
        </table>
        <!--[if mso]></td></tr></table><![endif]-->
      </td>
    </tr>
  </table>
</body>
</html>`))

type posterView struct {
	digestFilm
	Width, Height int
}

func posterCell(film any, width, height int) posterView {
	switch f := film.(type) {
	case *digestFilm:
		return posterView{digestFilm: *f, Width: width, Height: height}
	case digestFilm:
		return posterView{digestFilm: f, Width: width, Height: height}
	}
	return posterView{Width: width, Height: height}
}
