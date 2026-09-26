package accounts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"messeances/api/internal/accountavatar"
)

func TestAvatarPredecessorCommitBarrierIntegration(t *testing.T) {
	f, root := avatarFixture(t)
	a := f.complete(t, "barrier@example.com", "avatar_barrier")
	b, err := accountavatar.Normalize(t.Context(), avatarBytes(t), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	stage, err := f.service.avatars.Stage(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := f.service.avatars.Guard(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	path, err := stage.Publish(t.Context())
	unlock()
	stage.Done()
	if err != nil {
		t.Fatal(err)
	}
	old := f.now().Add(-2 * time.Hour)
	if err = os.Chtimes(filepath.Join(root, path), old, old); err != nil {
		t.Fatal(err)
	}
	// Simulate a predecessor process whose commit remains in flight after lock
	// handover. No process guard is held by that predecessor, only the DB barrier.
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var id int64
	if err = tx.QueryRow(t.Context(), `SELECT id FROM accounts WHERE email='barrier@example.com' FOR UPDATE`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err = avatarBarrier(t.Context(), tx); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), `UPDATE accounts SET avatar_path=$2,avatar_source='upload',avatar_revision=1 WHERE id=$1`, id, path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	if _, err = f.service.CleanupAvatars(ctx); err == nil {
		t.Fatal("GC did not wait for unfinished publication")
	}
	if _, err = os.Stat(filepath.Join(root, path)); err != nil {
		t.Fatal("GC deleted unfinished commit")
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if result, err := f.service.CleanupAvatars(t.Context()); err != nil || result.Deleted != 0 {
		t.Fatal("committed reference not preserved", result, err)
	}
	if _, err = f.service.Avatar(t.Context(), a.Cookie.Token, 1); err != nil {
		t.Fatal(err)
	}
}
func TestAvatarPendingPurgeOwnersIntegration(t *testing.T) {
	for _, owner := range []string{"cleanup", "register", "google"} {
		t.Run(owner, func(t *testing.T) {
			f, root := avatarFixture(t)
			const email = "expired-picture@example.com"
			pending := f.pending(t, email)
			// Attach a normalized synthetic image to a pending account without granting
			// settings authority, matching Google-imported pending media.
			b, err := accountavatar.Normalize(t.Context(), avatarBytes(t), "image/png")
			if err != nil {
				t.Fatal(err)
			}
			stage, err := f.service.avatars.Stage(t.Context(), b)
			if err != nil {
				t.Fatal(err)
			}
			unlock, _ := f.service.avatars.Guard(t.Context())
			path, err := stage.Publish(t.Context())
			unlock()
			stage.Done()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.pool.Exec(t.Context(), `UPDATE accounts SET avatar_path=$2,avatar_source='google',avatar_revision=1 WHERE email=$1`, email, path); err != nil {
				t.Fatal(err)
			}
			if _, err = f.service.Avatar(t.Context(), pending.Cookie.Token, 1); !errors.Is(err, ErrPending) {
				t.Fatal("pending image served")
			}
			f.advance(PendingLifetime + time.Second)
			err = nil
			switch owner {
			case "cleanup":
				_, err = f.service.Cleanup(t.Context())
			case "register":
				_, err = f.service.Register(t.Context(), email, testPassword)
			case "google":
				f.useGoogle(GoogleIdentity{Subject: "new-owner", Email: email, EmailVerified: true, EmailAuthoritative: true})
				loginGoogle(t, f)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(filepath.Join(root, path)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("purge owner retained media", owner, err)
			}
		})
	}
}

func TestAvatarStorageFailureAndRestartIntegration(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission fixture requires non-root UID")
	}
	f, root := avatarFixture(t)
	a := f.complete(t, "disk-picture@example.com", "disk_picture")
	upload(t, f, a.Cookie.Token)
	path, _, _ := storedAvatar(t, f, "disk-picture@example.com")
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0700) })
	b := avatarBytes(t)
	if _, err := f.service.UploadAvatar(t.Context(), a.Cookie.Token, func() ([]byte, string, error) { return b, "image/png", nil }); err == nil {
		t.Fatal("read-only disk accepted write")
	}
	current, _, rev := storedAvatar(t, f, "disk-picture@example.com")
	if current == nil || *current != *path || rev != 1 {
		t.Fatal("failed write changed state")
	}
	if _, err := f.service.RemoveAvatar(t.Context(), a.Cookie.Token); err != nil {
		t.Fatal("unlink failure reversed commit")
	}
	if _, err := f.service.Avatar(t.Context(), a.Cookie.Token, 1); !errors.Is(err, ErrAvatarNotFound) {
		t.Fatal("revoked image still served")
	}
	if _, err := os.Stat(filepath.Join(root, *path)); err != nil {
		t.Fatal("permission fixture unexpectedly unlinked")
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	old := f.now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, *path), old, old); err != nil {
		t.Fatal(err)
	}
	if result, err := f.service.CleanupAvatars(t.Context()); err != nil || result.Deleted != 1 {
		t.Fatal("failed unlink not collected", result, err)
	}
	upload(t, f, a.Cookie.Token)
	if err := f.service.CloseAvatars(); err != nil {
		t.Fatal(err)
	}
	reopened, err := accountavatar.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	f.service.avatars = reopened
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err = f.service.Avatar(t.Context(), a.Cookie.Token, 3); err != nil {
		t.Fatal("paired DB+media restart lost image", err)
	}
}

func TestAvatarSchemaInvariantIntegration(t *testing.T) {
	f, _ := avatarFixture(t)
	a := f.complete(t, "schema-photo@example.com", "schema_photo")
	f.complete(t, "schema-other@example.com", "schema_other")
	path, source, rev := storedAvatar(t, f, "schema-photo@example.com")
	if path != nil || source != "none" || rev != 0 {
		t.Fatal("migration default")
	}
	for _, values := range []string{`avatar_source='other'`, `avatar_revision=-1`, `avatar_path='../escape.png'`, `avatar_source='upload'`, `avatar_source='google',avatar_path='00000000000000000000000000000000.png'`, `avatar_source='removed',avatar_path='00000000000000000000000000000000.png'`} {
		if _, err := f.pool.Exec(t.Context(), `UPDATE accounts SET `+values+` WHERE email='schema-photo@example.com'`); err == nil {
			t.Fatal("invalid avatar state accepted", values)
		}
	}
	upload(t, f, a.Cookie.Token)
	path, _, _ = storedAvatar(t, f, "schema-photo@example.com")
	if _, err := f.pool.Exec(t.Context(), `UPDATE accounts SET avatar_path=$1,avatar_source='upload',avatar_revision=1 WHERE email='schema-other@example.com'`, *path); err == nil {
		t.Fatal("shared file path accepted")
	}
}
