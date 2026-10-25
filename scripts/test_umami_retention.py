"""Exercise the real retention SQL in an owned, disposable PostgreSQL 15 container.

Run via make test-umami-retention. Requires Docker and an already available
postgres:15-alpine image; never connects to an externally configured database.
"""

import contextlib
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import time
import unittest
import uuid


ROOT = Path(__file__).resolve().parents[1]
RETENTION = (ROOT / "deploy/umami-retention.sql").read_text()
SCHEMA = (ROOT / "scripts/fixtures/umami-retention.sql").read_text()
AUDIENCE = (
    "session", "session_link", "website_event", "event_data", "session_data",
    "revenue", "session_replay", "session_replay_saved", "heatmap_event",
)
PRESERVED = (
    "_prisma_migrations", "annotation", "api_key", "app_setting", "board", "link",
    "pixel", "report", "segment", "share", "team", "team_user", "two_factor_auth",
    "two_factor_backup_code", "two_factor_otp_used", "two_factor_rate_limit", "user",
    "website",
)
SESSION_ID = "11111111-1111-1111-1111-111111111111"
EVENT_ID = "22222222-2222-2222-2222-222222222222"


def command(*args, **kwargs):
    return subprocess.run(args, text=True, capture_output=True, timeout=60, **kwargs)


@contextlib.contextmanager
def disposable_database():
    name = "messeances-umami-retention-test-" + uuid.uuid4().hex
    ownership = uuid.uuid4().hex
    label_key = "messeances.umami-retention-test"
    env = dict(os.environ, POSTGRES_PASSWORD=secrets.token_hex(24))
    container = None
    try:
        result = command(
            "docker", "create", "--pull=never", "--name", name,
            "--label", f"{label_key}={ownership}", "--network", "none",
            "--tmpfs", "/var/lib/postgresql/data", "-e", "POSTGRES_PASSWORD",
            "postgres:15-alpine", env=env,
        )
        if result.returncode:
            raise RuntimeError("Disposable PostgreSQL creation failed; check Docker and the postgres:15-alpine image")
        container = result.stdout.strip()
        result = command("docker", "start", container)
        if result.returncode:
            raise RuntimeError("Disposable PostgreSQL startup failed")
        for _ in range(60):
            if command("docker", "exec", container, "pg_isready", "-U", "postgres").returncode == 0:
                break
            time.sleep(0.5)
        else:
            raise RuntimeError("Disposable PostgreSQL readiness timed out")
        yield container
    finally:
        # Recover an interrupted/ambiguous create by exact name and ownership.
        result = command("docker", "ps", "-aq", "--no-trunc", "--filter", f"name=^/{name}$")
        if result.returncode:
            raise RuntimeError("Cannot verify disposable PostgreSQL ownership for cleanup")
        found = result.stdout.strip()
        if found:
            result = command("docker", "inspect", "--format", "{{json .Config.Labels}}", found)
            if result.returncode or json.loads(result.stdout).get(label_key) != ownership:
                raise RuntimeError("Refusing to remove a container without matching test ownership")
            if container and container != found:
                raise RuntimeError("Disposable PostgreSQL identity changed")
            result = command("docker", "rm", "--force", found)
            if result.returncode:
                raise RuntimeError("Disposable PostgreSQL cleanup failed")
        result = command("docker", "ps", "-aq", "--filter", f"name=^/{name}$")
        if result.returncode or result.stdout.strip():
            raise RuntimeError("Disposable PostgreSQL disposal could not be verified")
        print("Owned disposable PostgreSQL container removed and absence verified.", flush=True)


class RetentionContractTests(unittest.TestCase):
    container = None

    def sql(self, source, expected=0):
        result = command(
            "docker", "exec", "-i", self.container, "psql", "-X", "--no-password",
            "-U", "postgres", "-d", "postgres", "-Atq", "-v", "ON_ERROR_STOP=1",
            input=source,
        )
        self.assertEqual(result.returncode, expected, result.stderr)
        return result

    def setUp(self):
        # This is always the container created above, with no persistent volume.
        self.sql("DROP SCHEMA public CASCADE; CREATE SCHEMA public;\n" + SCHEMA)

    def snapshot(self):
        return {
            table: self.sql(f'SELECT row_to_json(t)::text FROM public."{table}" t ORDER BY row_to_json(t)::text;').stdout
            for table in AUDIENCE
            if self.sql(f"SELECT to_regclass('public.{table}') IS NOT NULL;").stdout.strip() == "t"
        }

    def seed(self, timestamps=("CURRENT_TIMESTAMP - INTERVAL '26 months'", "CURRENT_TIMESTAMP", "NULL")):
        statements = []
        for table in AUDIENCE:
            for timestamp in timestamps:
                columns = ["created_at"]
                values = [timestamp]
                if table not in ("event_data", "session_replay_saved"):
                    columns.append("session_id")
                    values.append(f"'{SESSION_ID}'")
                if table == "website_event":
                    columns.append("event_id")
                    values.append(f"'{EVENT_ID}'")
                elif table == "event_data":
                    columns.append("website_event_id")
                    values.append(f"'{EVENT_ID}'")
                # Independent rows; parent inheritance gets its own test.
                if table == "session":
                    values[-1] = "gen_random_uuid()"
                if table == "website_event":
                    values[-1] = "gen_random_uuid()"
                statements.append(f'INSERT INTO public."{table}" ({", ".join(columns)}) VALUES ({", ".join(values)});')
        self.sql("\n".join(statements))

    def assert_blocked(self, diagnostic):
        before = self.snapshot()
        result = self.sql(RETENTION, expected=3)
        self.assertIn(diagnostic, result.stderr)
        self.assertEqual(self.snapshot(), before)

    def test_minimal_schema_without_release_metadata_or_configuration(self):
        self.sql(RETENTION)

    def test_all_tables_purge_old_and_null_rows_and_preserve_recent_rows(self):
        self.seed()
        self.sql(RETENTION)
        for table in AUDIENCE:
            with self.subTest(table=table):
                self.assertEqual(self.sql(f'SELECT count(*) FROM public."{table}";').stdout.strip(), "1")
                self.assertEqual(self.sql(f'SELECT count(*) FROM public."{table}" WHERE created_at > CURRENT_TIMESTAMP - INTERVAL \'25 months\';').stdout.strip(), "1")
        before = self.snapshot()
        self.sql(RETENTION)
        self.assertEqual(self.snapshot(), before)

    def test_recent_children_of_expired_sessions_and_events_are_purged(self):
        self.seed(("CURRENT_TIMESTAMP",))
        self.sql(f"INSERT INTO session VALUES ('{SESSION_ID}', CURRENT_TIMESTAMP - INTERVAL '26 months');\n"
                 f"INSERT INTO website_event VALUES ('{EVENT_ID}', '{SESSION_ID}', CURRENT_TIMESTAMP - INTERVAL '26 months');")
        self.sql(RETENTION)
        for table in AUDIENCE:
            expected = "1" if table in ("session", "session_replay_saved") else "0"
            with self.subTest(table=table):
                self.assertEqual(self.sql(f'SELECT count(*) FROM public."{table}";').stdout.strip(), expected)

    def test_extra_columns_and_changes_to_unused_columns_are_compatible(self):
        self.sql("ALTER TABLE session ADD COLUMN unused_id text; ALTER TABLE session ALTER COLUMN unused_id TYPE integer USING NULL;")
        self.sql(RETENTION)

    def test_stronger_timestamp_nullability_is_compatible(self):
        for table in AUDIENCE:
            self.sql(f'ALTER TABLE public."{table}" ALTER COLUMN created_at SET NOT NULL;')
        self.seed(("CURRENT_TIMESTAMP - INTERVAL '26 months'", "CURRENT_TIMESTAMP"))
        self.sql(RETENTION)
        for table in AUDIENCE:
            self.assertEqual(self.sql(f'SELECT count(*) FROM public."{table}";').stdout.strip(), "1")

    def test_reviewed_configuration_and_migration_metadata_are_preserved(self):
        for table in PRESERVED:
            self.sql(f'CREATE TABLE public."{table}" (value text, created_at timestamptz); INSERT INTO public."{table}" VALUES (\'keep\', CURRENT_TIMESTAMP - INTERVAL \'26 months\'), (\'keep null\', NULL);')
        before = {
            table: self.sql(f'SELECT row_to_json(t)::text FROM public."{table}" t ORDER BY value;').stdout
            for table in PRESERVED
        }
        self.seed()
        self.sql(RETENTION)
        for table in PRESERVED:
            with self.subTest(table=table):
                self.assertEqual(self.sql(f'SELECT row_to_json(t)::text FROM public."{table}" t ORDER BY value;').stdout, before[table])

    def test_unclassified_table_blocks_before_deletion(self):
        self.seed()
        self.sql("CREATE TABLE public.new_analytics (id integer);")
        self.assert_blocked("unclassified tables: new_analytics; review their retention policy")

    def test_missing_required_table_blocks_before_deletion(self):
        self.seed()
        self.sql("DROP TABLE session_replay;")
        self.assert_blocked("missing required tables: session_replay")

    def test_missing_required_column_blocks_before_deletion(self):
        self.seed()
        self.sql("ALTER TABLE website_event DROP COLUMN session_id;")
        self.assert_blocked("website_event.session_id (expected uuid, found missing)")

    def test_incompatible_required_column_types_block_before_deletion(self):
        for alteration, diagnostic in (
            ("ALTER TABLE session ALTER COLUMN created_at TYPE timestamp without time zone", "session.created_at (expected timestamp with time zone, found timestamp without time zone)"),
            ("ALTER TABLE event_data ALTER COLUMN website_event_id TYPE text", "event_data.website_event_id (expected uuid, found text)"),
        ):
            with self.subTest(alteration=alteration):
                self.setUp()
                self.seed()
                self.sql(alteration)
                self.assert_blocked(diagnostic)

    def test_error_during_later_deletion_rolls_back_earlier_deletions(self):
        self.seed()
        self.sql("""CREATE FUNCTION public.reject_delete() RETURNS trigger LANGUAGE plpgsql AS $$
            BEGIN RAISE EXCEPTION 'synthetic purge failure'; END; $$;
            CREATE TRIGGER reject_delete BEFORE DELETE ON public.session
            FOR EACH ROW EXECUTE FUNCTION public.reject_delete();""")
        self.assert_blocked("synthetic purge failure")


def main():
    def interrupt(_signum, _frame):
        raise KeyboardInterrupt

    previous = signal.signal(signal.SIGTERM, interrupt)
    try:
        with disposable_database() as container:
            RetentionContractTests.container = container
            suite = unittest.defaultTestLoader.loadTestsFromTestCase(RetentionContractTests)
            result = unittest.TextTestRunner(verbosity=2).run(suite)
        return 0 if result.wasSuccessful() else 1
    finally:
        signal.signal(signal.SIGTERM, previous)


if __name__ == "__main__":
    raise SystemExit(main())
