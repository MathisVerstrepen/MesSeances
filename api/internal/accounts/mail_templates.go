package accounts

import (
	"bytes"
	"html/template"

	"messeances/api/internal/accountmail"
)

type mailContent struct {
	Label       string
	Title       string
	Body        string
	ActionLabel string
	ActionURL   string
	IconURL     string
}

func actionMailMessage(origin, recipient, link string, purpose TokenPurpose) (accountmail.Message, error) {
	subject := "Confirmez votre demande MesSeances"
	content := mailContent{
		Label:       "Mon compte",
		Title:       "Réinitialisez votre mot de passe",
		Body:        "Pour confirmer votre demande, ouvrez ce lien puis validez le formulaire :",
		ActionLabel: "Réinitialiser mon mot de passe",
		ActionURL:   link,
	}
	switch purpose {
	case TokenVerification:
		content.Title = "Vérifiez votre adresse email"
		content.ActionLabel = "Vérifier mon adresse email"
	case TokenEmailChange:
		content.Title = "Confirmez votre nouvelle adresse"
		content.ActionLabel = "Confirmer mon nouvel email"
	case TokenEmailStepUp:
		subject = "Confirmez votre identité MesSeances"
		content.Label = "Sécurité"
		content.Title = "Confirmez votre identité"
		content.Body = "Pour confirmer votre identité, ouvrez ce lien puis validez le formulaire :"
		content.ActionLabel = "Confirmer mon identité"
	}
	return renderMailMessage(origin, accountmail.Message{
		Recipient: recipient,
		Subject:   subject,
		Text:      content.Body + "\n" + link,
	}, content)
}

func securityMailMessage(origin, recipient, text string) (accountmail.Message, error) {
	return renderMailMessage(origin, accountmail.Message{
		Recipient: recipient,
		Subject:   "Sécurité de votre compte MesSeances",
		Text:      text,
	}, mailContent{
		Label: "Sécurité",
		Title: "Votre compte MesSeances",
		Body:  text,
	})
}

func renderMailMessage(origin string, message accountmail.Message, content mailContent) (accountmail.Message, error) {
	// The configured origin is trusted; never derive this static resource URL
	// from the recipient or action link, which can contain private account data.
	content.IconURL = origin + "/pwa-64x64.png"
	var body bytes.Buffer
	if err := accountMailTemplate.Execute(&body, content); err != nil {
		return accountmail.Message{}, err
	}
	message.HTML = body.String()
	return message, nil
}

// Keep all dynamic values as strings so html/template escapes both text and URL
// attributes. The action URL appears only in its link, never in preview metadata.
var accountMailTemplate = template.Must(template.New("account-mail").Parse(`<!doctype html>
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
              <p style="margin:0 0 20px;font-family:'Courier New',Courier,monospace;font-size:12px;line-height:20px;font-weight:bold;letter-spacing:1px;text-transform:uppercase;"><span style="background-color:#a8bfa3;color:#27272a;padding:4px 8px;">{{.Label}}</span></p>
              <h1 style="margin:0 0 24px;font-size:32px;line-height:38px;font-weight:900;letter-spacing:-1px;">{{.Title}}</h1>
              <p style="margin:0;font-size:16px;line-height:26px;overflow-wrap:anywhere;">{{.Body}}</p>
              {{if .ActionURL}}
              <table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin-top:28px;border-collapse:collapse;">
                <tr>
                  <td align="center" bgcolor="#991b1b" style="border:2px solid #27272a;mso-padding-alt:14px 20px;">
                    <a href="{{.ActionURL}}" style="display:inline-block;padding:14px 20px;color:#ffffff;background-color:#991b1b;font-family:Arial,Helvetica,sans-serif;font-size:15px;line-height:22px;font-weight:bold;text-decoration:underline;">{{.ActionLabel}}</a>
                  </td>
                </tr>
              </table>
              {{end}}
            </td>
          </tr>
          <tr>
            <td bgcolor="#fcfaf8" style="padding:20px 24px;border-top:2px solid #27272a;color:#27272a;font-family:'Courier New',Courier,monospace;font-size:11px;line-height:18px;font-weight:bold;letter-spacing:1px;text-transform:uppercase;">MesSeances · Mon compte</td>
          </tr>
        </table>
        <!--[if mso]></td></tr></table><![endif]-->
      </td>
    </tr>
  </table>
</body>
</html>`))
