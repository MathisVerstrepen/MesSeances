package accounts

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"messeances/api/internal/accountavatar"
)

func TestGoogleAvatarAuthorizedFlowsIntegration(t *testing.T) {
	f, _ := avatarFixture(t)
	a := f.complete(t, "linked-photo@example.com", "linked_photo")
	identity := GoogleIdentity{Subject: "linked-photo", Email: "linked-photo@example.com", EmailVerified: true, EmailAuthoritative: true, Picture: "https://lh3.googleusercontent.com/photo"}
	f.useGoogle(identity)
	calls := 0
	f.service.pictures = pictureFixture{func(context.Context, string) ([]byte, error) {
		calls++
		return accountavatar.Normalize(t.Context(), avatarBytes(t), "image/png")
	}}
	start, state := startGoogle(t, f, "", GoogleStart{Mode: FlowLogin})
	if _, err := f.service.GoogleCallback(t.Context(), state, start.Browser.Token, "valid", ""); !errors.Is(err, ErrGoogleEmailInUse) || calls != 0 {
		t.Fatal("collision fetched photo")
	}
	proof, err := f.service.ReauthPassword(t.Context(), a.Cookie.Token, testPassword, ActionGoogleLink, "")
	if err != nil {
		t.Fatal(err)
	}
	start, state = startGoogle(t, f, proof.Cookie.Token, GoogleStart{Mode: FlowLink, Grant: proof.Grant})
	linked, err := f.service.GoogleCallback(t.Context(), state, start.Browser.Token, "valid", proof.Cookie.Token)
	if err != nil {
		t.Fatal(err)
	}
	path, source, rev := storedAvatar(t, f, identity.Email)
	if calls != 1 || path == nil || source != "google" || rev != 1 {
		t.Fatal("post-link revision did not import")
	}
	// Preserve stored image when unlinking the identity.
	proof, err = f.service.ReauthPassword(t.Context(), linked.Session.Cookie.Token, testPassword, ActionGoogleUnlink, "")
	if err != nil {
		t.Fatal(err)
	}
	unlinked, err := f.service.UnlinkGoogle(t.Context(), proof.Cookie.Token, proof.Grant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Avatar(t.Context(), unlinked.Token, 1); err != nil {
		t.Fatal("unlink removed stored photo")
	}
	// Google reauthentication must not fetch, even with untouched empty media.
	g := completeGoogle(t, f, "reauth-photo@example.com", "reauth_photo")
	f.service.google.(*fakeGoogle).identity.Picture = identity.Picture
	googleProof(t, f, g.Cookie.Token, ActionDelete, "")
	if calls != 1 {
		t.Fatal("reauth fetched picture")
	}
}
func TestGoogleAvatarQuotaAndRetryIntegration(t *testing.T) {
	f, _ := avatarFixture(t)
	a := completeGoogle(t, f, "retry-photo@example.com", "retry_photo")
	var snapshot account
	if err := f.service.store.withTransaction(t.Context(), func(tx pgx.Tx) error {
		var err error
		snapshot, _, err = f.service.authorize(t.Context(), tx, a.Cookie.Token, true)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	candidate := &avatarImport{id: snapshot.id, revision: snapshot.revision, avatarRevision: snapshot.avatarRevision, subject: "subject-retry_photo", picture: "https://lh3.googleusercontent.com/photo"}
	calls := 0
	f.service.pictures = pictureFixture{func(context.Context, string) ([]byte, error) {
		calls++
		return nil, errors.New("synthetic fetch failure")
	}}
	for range 11 {
		f.service.importAvatar(t.Context(), candidate)
	}
	if calls != 10 {
		t.Fatal("import quota", calls)
	}
	path, source, rev := storedAvatar(t, f, "retry-photo@example.com")
	if path != nil || source != "none" || rev != 0 {
		t.Fatal("optional failure mutated intent")
	}
}
