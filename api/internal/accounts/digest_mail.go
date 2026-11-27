package accounts

import (
	"bytes"
	"html/template"
	"strconv"
	"strings"

	"messeances/api/internal/accountmail"
)

type digestEntry struct {
	Context, Text, URL string
}

type digestContent struct {
	Title, Intro, More, IconURL, InboxURL, SettingsURL, UnsubscribeURL, Cadence string
	Entries                                                                     []digestEntry
}

func digestMailMessage(origin, recipient, frequency string, items []pushPayload, total int, oneClickURL, unsubscribePageURL string) (accountmail.Message, error) {
	cadence := "quotidien"
	if frequency == DigestWeekly {
		cadence = "hebdomadaire"
	}
	subject := "Vos alertes MesSeances : " + strconv.Itoa(total) + " " + plural(total, "notification", "notifications")
	content := digestContent{
		Title:          "Vos alertes",
		Intro:          strconv.Itoa(total) + " " + plural(total, "notification non lue", "notifications non lues") + " depuis votre dernier résumé.",
		IconURL:        origin + "/pwa-64x64.png",
		InboxURL:       origin + "/compte/notifications",
		SettingsURL:    origin + "/compte/notifications?onglet=alertes",
		UnsubscribeURL: unsubscribePageURL,
		Cadence:        cadence,
	}
	if total > len(items) {
		content.More = "Et " + strconv.Itoa(total-len(items)) + " de plus dans votre boîte de notifications."
	}
	var text strings.Builder
	text.WriteString(content.Intro + "\n\n")
	for _, item := range items {
		entry := digestEntry{Context: item.Title, Text: item.Body, URL: origin + item.URL}
		content.Entries = append(content.Entries, entry)
		text.WriteString("- " + entry.Text + "\n  " + entry.URL + "\n")
	}
	if content.More != "" {
		text.WriteString("\n" + content.More + "\n")
	}
	text.WriteString("\nToutes vos notifications : " + content.InboxURL + "\n")
	text.WriteString("Gérer vos alertes : " + content.SettingsURL + "\n")
	text.WriteString("\nVous recevez ce résumé " + cadence + " car vous l’avez activé. Se désabonner : " + unsubscribePageURL + "\n")
	var body bytes.Buffer
	if err := digestMailTemplate.Execute(&body, content); err != nil {
		return accountmail.Message{}, err
	}
	return accountmail.Message{Recipient: recipient, Subject: subject, Text: text.String(), HTML: body.String(), UnsubscribeURL: oneClickURL}, nil
}

// Same visual frame as account mail; every dynamic value is escaped by
// html/template, including URL attributes.
var digestMailTemplate = template.Must(template.New("digest-mail").Parse(`<!doctype html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
</head>
<body style="margin:0;padding:0;background-color:#f8f7f2;color:#27272a;font-family:Arial,Helvetica,sans-serif;-webkit-text-size-adjust:100%;">
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
            <td bgcolor="#ffffff" style="padding:32px 24px;color:#27272a;font-family:Arial,Helvetica,sans-serif;">
              <p style="margin:0 0 20px;font-family:'Courier New',Courier,monospace;font-size:12px;line-height:20px;font-weight:bold;letter-spacing:1px;text-transform:uppercase;"><span style="background-color:#a8bfa3;color:#27272a;padding:4px 8px;">Résumé {{.Cadence}}</span></p>
              <h1 style="margin:0 0 12px;font-size:32px;line-height:38px;font-weight:900;letter-spacing:-1px;">{{.Title}}</h1>
              <p style="margin:0 0 24px;font-size:16px;line-height:26px;">{{.Intro}}</p>
              <table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;border-collapse:collapse;border-top:2px solid #27272a;">
                {{range .Entries}}
                <tr>
                  <td style="padding:14px 0;border-bottom:1px solid #d4d4d8;">
                    <p style="margin:0 0 4px;font-family:'Courier New',Courier,monospace;font-size:11px;line-height:16px;font-weight:bold;letter-spacing:1px;text-transform:uppercase;color:#71717a;">{{.Context}}</p>
                    <a href="{{.URL}}" style="color:#27272a;font-size:16px;line-height:24px;font-weight:bold;text-decoration:underline;overflow-wrap:anywhere;">{{.Text}}</a>
                  </td>
                </tr>
                {{end}}
              </table>
              {{if .More}}<p style="margin:16px 0 0;font-size:14px;line-height:22px;">{{.More}}</p>{{end}}
              <table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin-top:28px;border-collapse:collapse;">
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
              Vous recevez ce résumé {{.Cadence}} car vous l’avez activé.
              <a href="{{.SettingsURL}}" style="color:#27272a;">Gérer mes alertes</a> ·
              <a href="{{.UnsubscribeURL}}" style="color:#27272a;">Se désabonner</a>
            </td>
          </tr>
        </table>
        <!--[if mso]></td></tr></table><![endif]-->
      </td>
    </tr>
  </table>
</body>
</html>`))
