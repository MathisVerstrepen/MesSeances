package database

import (
	"strings"
	"testing"
	"time"
)

func TestTheaterImagesMigrationIntegration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "upgrade057"}[upgrade], func(t *testing.T) {
			ctx := t.Context()
			pool, _ := newMigrationTestPool(t, ctx, "movieflow_theater_images_")
			if upgrade {
				installMigrationPrefix(t, ctx, pool, 57, "057_movie_metacritic_id.sql")
			}
			if e := RunMigrations(ctx, pool); e != nil {
				t.Fatal(e)
			}
			var applied time.Time
			if e := pool.QueryRow(ctx, "SELECT applied_at FROM movieflow_schema_migrations WHERE version=58").Scan(&applied); e != nil {
				t.Fatal(e)
			}
			if e := RunMigrations(ctx, pool); e != nil {
				t.Fatal(e)
			}
			var repeated time.Time
			if e := pool.QueryRow(ctx, "SELECT applied_at FROM movieflow_schema_migrations WHERE version=58").Scan(&repeated); e != nil || !repeated.Equal(applied) {
				t.Fatal("migration reapplied", e)
			}
			var total int
			if e := pool.QueryRow(ctx, "SELECT count(*) FROM theater_images").Scan(&total); e != nil || total != 0 {
				t.Fatal("migration backfilled", e)
			}
			if _, e := pool.Exec(ctx, "INSERT INTO theater_images(provider,provider_theater_id) VALUES('ugc','25'),('kinepolis','25')"); e != nil {
				t.Fatal("provider identity separation", e)
			}
			var revision int64
			if e := pool.QueryRow(ctx, "SELECT image_revision FROM theater_images WHERE provider='ugc'").Scan(&revision); e != nil || revision != 0 {
				t.Fatal(e)
			}
			key := strings.Repeat("a", 32) + ".webp"
			if _, e := pool.Exec(ctx, "UPDATE theater_images SET image_revision=1,file_key=$1,width=1600,height=1,size_bytes=1048576 WHERE provider='ugc'", key); e != nil {
				t.Fatal("valid boundary", e)
			}
			for _, sql := range []string{
				"UPDATE theater_images SET image_revision=-1 WHERE provider='ugc'",
				"UPDATE theater_images SET image_revision=9007199254740992 WHERE provider='ugc'",
				"UPDATE theater_images SET image_revision=0 WHERE provider='ugc'",
				"UPDATE theater_images SET file_key='../image.webp' WHERE provider='ugc'",
				"UPDATE theater_images SET width=NULL WHERE provider='ugc'",
				"UPDATE theater_images SET width=1601 WHERE provider='ugc'",
				"UPDATE theater_images SET height=0 WHERE provider='ugc'",
				"UPDATE theater_images SET size_bytes=1048577 WHERE provider='ugc'",
				"UPDATE theater_images SET file_key=NULL WHERE provider='ugc'",
				"UPDATE theater_images SET provider='unknown' WHERE provider='ugc'",
				"UPDATE theater_images SET provider_theater_id='' WHERE provider='ugc'",
				"UPDATE theater_images SET provider_theater_id='   ' WHERE provider='ugc'",
				"UPDATE theater_images SET provider_theater_id=repeat('a',129) WHERE provider='ugc'",
			} {
				if _, e := pool.Exec(ctx, sql); e == nil {
					t.Fatalf("invalid constraint accepted %s", sql)
				}
			}
			if _, e := pool.Exec(ctx, "UPDATE theater_images SET image_revision=1,file_key=$1,width=1,height=1,size_bytes=1 WHERE provider='kinepolis'", key); e == nil {
				t.Fatal("duplicate file key accepted")
			}
			if _, e := pool.Exec(ctx, "UPDATE theater_images SET image_revision=2,file_key=NULL,width=NULL,height=NULL,size_bytes=NULL WHERE provider='ugc'"); e != nil {
				t.Fatal("tombstone", e)
			}
			if _, e := pool.Exec(ctx, "UPDATE theater_images SET image_revision=9007199254740991 WHERE provider='ugc'"); e != nil {
				t.Fatal("max revision", e)
			}
			var fk int
			if e := pool.QueryRow(ctx, "SELECT count(*) FROM pg_constraint WHERE conrelid='theater_images'::regclass AND contype='f'").Scan(&fk); e != nil || fk != 0 {
				t.Fatal("generation ownership coupled", e)
			}
		})
	}
}
