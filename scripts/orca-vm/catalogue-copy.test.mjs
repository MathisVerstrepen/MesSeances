import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { setTimeout } from 'node:timers/promises'

const scripts = dirname(fileURLToPath(import.meta.url))
const root = resolve(scripts, '../..')
function command(bin, args, input, env = {}) {
  return spawnSync(bin, args, {
    cwd: root, env: { ...process.env, ...env }, input,
    encoding: 'utf8', timeout: 300000, maxBuffer: 2 * 1024 * 1024,
  })
}
function run(bin, args, input, env) {
  const result = command(bin, args, input, env)
  assert.equal(result.status, 0, `${bin}: ${result.stderr}`)
  return result.stdout.trim()
}
const docker = (...args) => run('docker', args)
const sql = (container, query) => docker('exec', container, 'psql', '-X', '-U', 'movieflow', '-d', 'movieflow', '-v', 'ON_ERROR_STOP=1', '-Atc', query)
const recipe = (action, input, env) => run(`${scripts}/docker-${action}.sh`, [], input, env)
const payload = item => JSON.stringify({ recipeResult: item })
const summary = container => sql(container, `SELECT json_build_array(
  (SELECT count(*) FROM public_movies), (SELECT count(*) FROM showtimes),
  (SELECT version FROM schedule_snapshot), (SELECT version FROM movie_enrichment_state),
  (SELECT version FROM theater_location_state), (SELECT max(version) FROM movieflow_schema_migrations),
  (SELECT last_value FROM public_movies_id_seq), (SELECT last_value FROM local_movie_groups_id_seq))`)

test('new workspaces copy only catalogue data, validate readiness and clean failed copies', {
  skip: process.env.ORCA_DOCKER_INTEGRATION !== '1', timeout: 900000,
}, async () => {
  const prefix = `catalogue-test-${randomUUID().slice(0, 8)}`
  const environments = []
  const allowlist = readFileSync(`${scripts}/catalogue-tables.txt`, 'utf8').trim().split('\n')
  const assertClean = id => {
    const instance = `messeances-orca-local-docker-${id}`
    assert.equal(docker('ps', '-a', '--filter', `label=fr.messeances.orca.instance=${instance}`, '--format', '{{.ID}}'), '')
    assert.equal(docker('network', 'ls', '--filter', `name=^${instance}$`, '--format', '{{.ID}}'), '')
    assert.ok(!readdirSync(`${scripts}/.local`).some(name => name.startsWith('catalogue.')))
  }
  const create = (suffix, source) => {
    const env = { ORCA_RECIPE_ID: 'local-docker', ORCA_VM_INSTANCE_ID: `${prefix}-${suffix}` }
    if (source) env.ORCA_DOCKER_SOURCE_POSTGRES_CONTAINER = source
    const item = JSON.parse(recipe('create', undefined, env))
    environments.push(item)
    return item
  }
  const failedCreate = (suffix, source, expected) => {
    const id = `${prefix}-${suffix}`
    const result = command(`${scripts}/docker-create.sh`, [], undefined, {
      ORCA_RECIPE_ID: 'local-docker', ORCA_VM_INSTANCE_ID: id,
      ORCA_DOCKER_SOURCE_POSTGRES_CONTAINER: source,
    })
    assert.notEqual(result.status, 0)
    assert.equal(result.stdout.trim(), '')
    assert.match(result.stderr, expected)
    assertClean(id)
  }
  try {
    failedCreate('missing', `${prefix}-absent`, /No such (object|container)/i)
    const a = create('a')
    const dbA = `${a.userData.instance}-postgres`
    assert.ok(Number(sql(dbA, 'SELECT count(*) FROM public_movies')) > 0)
    const tables = sql(dbA, "SELECT tablename FROM pg_tables WHERE schemaname='public'").split('\n')
    for (const table of tables.filter(name => !allowlist.includes(name))) {
      assert.match(table, /^[a-z][a-z0-9_]+$/)
      assert.equal(sql(dbA, `SELECT count(*) FROM public.${table}`), '0', `${table} must be empty`)
    }
    sql(dbA, `INSERT INTO accounts(email, created_at, email_verified_at, verification_source)
      VALUES ('orca-fixture@example.test', now(), now(), 'email');
      INSERT INTO account_sessions(token_digest, account_id, auth_revision, created_at, expires_at, last_seen_at, scope)
      SELECT decode(repeat('01',32),'hex'),id,1,now(),now()+interval '1 hour',now(),'complete' FROM accounts;
      INSERT INTO account_tokens(token_digest, account_id, auth_revision, purpose, created_at, expires_at)
      SELECT decode(repeat('02',32),'hex'),id,1,'password_reset',now(),now()+interval '1 hour' FROM accounts;
      CREATE TABLE orca_private_probe(value text);
      INSERT INTO orca_private_probe VALUES ('must-not-copy');`)
    const b = create('b', dbA)
    const dbB = `${b.userData.instance}-postgres`
    assert.equal(summary(dbB), summary(dbA), 'catalogue counts, publications, migrations and identity sequences')
    for (const table of ['accounts', 'account_sessions', 'account_tokens', 'orca_private_probe']) {
      assert.equal(sql(dbB, `SELECT count(*) FROM ${table}`), '0', `exclude ${table}`)
    }
    recipe('suspend', payload(b))
    const resumed = JSON.parse(recipe('resume', payload(b)))
    environments[1] = resumed
    assert.equal(resumed.userData.hostKey, b.userData.hostKey)
    assert.equal(summary(dbB), summary(dbA), 'resume preserves seeded database')

    docker('exec', '--user', 'mathis', resumed.userData.resourceId, 'bash', '-lc',
      'git -C /home/mathis/projects/messeances worktree add --detach /home/mathis/catalogue-check HEAD')
    docker('exec', '-d', '--user', 'mathis', resumed.userData.resourceId, 'bash', '-lc',
      'cd /home/mathis/catalogue-check && exec orca-dev >/tmp/orca-catalogue-dev.log 2>&1')
    let ready = false
    for (let attempt = 0; attempt < 120; attempt++) {
      await setTimeout(1000)
      try {
        const api = await fetch(`${resumed.userData.apiUrl}/healthz`, { signal: AbortSignal.timeout(2000) })
        if (!api.ok) continue
        const page = await fetch(resumed.userData.webUrl, { headers: { accept: 'text/html' }, signal: AbortSignal.timeout(10000) })
        if (!page.ok) continue
        assert.match(await page.text(), /MesSeances/)
        const catalogue = await fetch(`${resumed.userData.webUrl}/api/v1/movies`, { signal: AbortSignal.timeout(10000) })
        assert.equal(catalogue.status, 200)
        ready = true
        break
      } catch (error) {
        if (error.code === 'ERR_ASSERTION') throw error
      }
    }
    assert.ok(ready, 'linked-worktree API, SSR homepage and same-origin catalogue return 200')

    // Fixture-only FK forces a real post-data restoration failure: private
    // account rows are excluded even when an allowed table points at them.
    sql(dbA, `ALTER TABLE public_movies ADD COLUMN orca_account_id bigint REFERENCES accounts(id);
      UPDATE public_movies SET orca_account_id=(SELECT id FROM accounts LIMIT 1)
      WHERE id=(SELECT min(id) FROM public_movies);`)
    failedCreate('restore', dbA, /pg_restore: error/)
    sql(dbA, 'ALTER TABLE public_movies DROP COLUMN orca_account_id; DELETE FROM showtimes;')
    assert.ok(Number(sql(dbB, 'SELECT count(*) FROM showtimes')) > 0, 'workspace edits are independent')
    failedCreate('stale', dbA, /no usable published future showtimes/)
    docker('exec', dbA, 'sh', '-c', 'chmod 000 "$(command -v pg_dump)"')
    failedCreate('dump', dbA, /pg_dump: Permission denied/)
  } finally {
    for (const item of environments.reverse()) {
      recipe('destroy', payload(item))
      assertClean(item.userData.instance.replace('messeances-orca-local-docker-', ''))
    }
  }
})
