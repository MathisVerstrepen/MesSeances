package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"messeances/api/internal/schedule"
	"messeances/api/internal/syncsource"
)

func TestSyncSourcesStoreAndConstraintsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "sync_sources_")
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var registered bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM providers WHERE key='ticketingcine')`).Scan(&registered); err != nil || !registered {
		t.Fatal("ticketingcine provider not registered", err)
	}
	var seeded, enabled int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE enabled) FROM sync_sources WHERE provider='ticketingcine' AND source_key ~ '^EMS[0-9]{4}$' AND cnc_id IS NOT NULL AND name <> ''`).Scan(&seeded, &enabled); err != nil || seeded != 134 || enabled != 0 {
		t.Fatal("seeded TicketingCiné sources", seeded, enabled, err)
	}
	var scare, scareEnabled int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE enabled) FROM sync_sources WHERE provider='scare' AND source_key ~ '^[1-9][0-9]*$' AND cnc_id IS NOT NULL AND name <> ''`).Scan(&scare, &scareEnabled); err != nil || scare != 94 || scareEnabled != 0 {
		t.Fatal("seeded SCARE sources", scare, scareEnabled, err)
	}
	var webedia, webediaEnabled int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE enabled) FROM sync_sources WHERE provider='webedia' AND source_key ~ '^[A-Z][A-Z0-9]{4}$' AND website ~ '^https://[a-z0-9.-]+/$' AND cnc_id IS NOT NULL`).Scan(&webedia, &webediaEnabled); err != nil || webedia != 67 || webediaEnabled != 0 {
		t.Fatal("seeded Webedia sources", webedia, webediaEnabled, err)
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_sources`).Scan(&total); err != nil || total != seeded+scare+webedia {
		t.Fatal("unexpected seeded sources", total, err)
	}
	var shared int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_sources a JOIN sync_sources b ON a.cnc_id = b.cnc_id AND a.provider < b.provider`).Scan(&shared); err != nil || shared != 0 {
		t.Fatal("a cinema is seeded for two providers", shared, err)
	}
	for _, statement := range []string{
		`INSERT INTO sync_sources (provider,source_key,enabled,name,website,cnc_id) VALUES ('ticketingcine','EMS1234',true,'Cinéma A','https://cinema-a.example/','123456')`,
		`INSERT INTO sync_sources (provider,source_key,enabled,params) VALUES ('ticketingcine','EMS0042',true,'{"note":"x"}')`,
		`INSERT INTO sync_sources (provider,source_key,enabled) VALUES ('ticketingcine','EMS9999',false)`,
		`INSERT INTO sync_sources (provider,source_key,enabled) VALUES ('ugc','42',true)`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(statement, err)
		}
	}
	sources, err := syncsource.NewPostgresStore(pool).Enabled(ctx, schedule.ProviderTicketingcine)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].Key != "EMS0042" || sources[1].Key != "EMS1234" || sources[1].Name != "Cinéma A" || sources[1].Website != "https://cinema-a.example/" || sources[1].CNCID != "123456" || string(sources[0].Params) != `{"note": "x"}` || sources[0].Provider != schedule.ProviderTicketingcine {
		t.Fatalf("sources=%+v", sources)
	}
	for _, statement := range []string{
		`INSERT INTO sync_sources (provider,source_key) VALUES ('unregistered','EMS1')`,
		`INSERT INTO sync_sources (provider,source_key) VALUES ('ticketingcine','bad key')`,
		`INSERT INTO sync_sources (provider,source_key) VALUES ('ticketingcine','EMS1234')`,
		`INSERT INTO sync_sources (provider,source_key,website) VALUES ('ticketingcine','EMS1','http://cinema.example/')`,
		`INSERT INTO sync_sources (provider,source_key,name) VALUES ('ticketingcine','EMS2',' padded ')`,
		`INSERT INTO sync_sources (provider,source_key,params) VALUES ('ticketingcine','EMS3','[]')`,
		`INSERT INTO sync_sources (provider,source_key,cnc_id) VALUES ('ticketingcine','EMS4','12 34')`,
	} {
		if _, err := pool.Exec(ctx, statement); err == nil {
			t.Fatal("invalid source accepted", statement)
		}
	}
}

func TestSyncSourcesAdminTogglesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, _ := newMigrationTestPool(t, ctx, "sync_sources_admin_")
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := syncsource.NewPostgresStore(pool)
	all, err := store.List(ctx)
	if err != nil || len(all) != 134+94+67 || all[0].Provider != schedule.ProviderScare || all[0].Enabled {
		t.Fatalf("list=%d first=%+v err=%v", len(all), all[0], err)
	}
	updated, err := store.SetEnabled(ctx, schedule.ProviderTicketingcine, "EMS0963", true)
	if err != nil || !updated.Enabled || updated.Name == "" || updated.UpdatedAt.Before(all[0].UpdatedAt) {
		t.Fatalf("toggle=%+v err=%v", updated, err)
	}
	if _, err := store.SetEnabled(ctx, schedule.ProviderTicketingcine, "EMS0000", true); !errors.Is(err, syncsource.ErrNotFound) {
		t.Fatalf("missing source err=%v", err)
	}
	if n, err := store.SetAllEnabled(ctx, schedule.ProviderScare, true); err != nil || n != 94 {
		t.Fatalf("provider bulk=%d err=%v", n, err)
	}
	enabled, err := store.Enabled(ctx, schedule.ProviderScare)
	if err != nil || len(enabled) != 94 {
		t.Fatalf("enabled=%d err=%v", len(enabled), err)
	}
	if n, err := store.SetAllEnabled(ctx, "", true); err != nil || n != 134+67-1 {
		t.Fatalf("all bulk=%d err=%v", n, err)
	}
	if n, err := store.SetAllEnabled(ctx, "", false); err != nil || n != 134+94+67 {
		t.Fatalf("disable all=%d err=%v", n, err)
	}
}
