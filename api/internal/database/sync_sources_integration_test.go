package database

import (
	"context"
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
	var seeded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sync_sources`).Scan(&seeded); err != nil || seeded != 0 {
		t.Fatal("sources seeded", seeded, err)
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
