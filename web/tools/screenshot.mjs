import { spawn } from 'node:child_process'
import { constants as fsConstants } from 'node:fs'
import { access, mkdtemp, readFile, rename, rm, stat, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { basename, dirname, extname, isAbsolute, join, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { parseEnv } from 'node:util'

const rootDirectory = fileURLToPath(new URL('../..', import.meta.url))
const envFile = join(rootDirectory, '.env')
const temporaryPrefix = join(tmpdir(), 'movieflow-screenshot-')
const adminCookieName = 'movieflow_admin_session'
const adminCookiePath = '/api/v1/admin'
const adminSessionSeconds = 12 * 60 * 60
const cdpCommandTimeoutMilliseconds = 15000

let chrome
let profileDirectory
let partialOutput
let cleaningUp

class CaptureError extends Error {}

function fail(message) {
  throw new CaptureError(message)
}

function cleanText(name, fallback) {
  const value = process.env[name] ?? fallback
  if (!value || /[\u0000-\u001f\u007f]/u.test(value)) fail(`${name} is invalid.`)
  return value
}

function parseInteger(name, fallback, minimum, maximum) {
  const value = cleanText(name, fallback)
  if (!/^(0|[1-9]\d*)$/u.test(value)) fail(`${name} must be an integer from ${minimum} to ${maximum}.`)
  const parsed = Number(value)
  if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) {
    fail(`${name} must be an integer from ${minimum} to ${maximum}.`)
  }
  return parsed
}

function parseURL(name, fallback) {
  const value = cleanText(name, fallback)
  let parsed
  try {
    parsed = new URL(value)
  } catch {
    fail(`${name} must be a valid HTTP or HTTPS URL.`)
  }
  if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password) {
    fail(`${name} must be a credential-free HTTP or HTTPS URL.`)
  }
  return parsed
}

function parseConfig() {
  const targetURL = parseURL('URL', 'http://localhost:3000/')
  const apiURL = parseURL('API_URL', 'http://localhost:8080')
  const outputValue = cleanText('OUTPUT', '/tmp/opencode/movieflow-screenshot.png')
  if (extname(outputValue).toLowerCase() !== '.png') fail('OUTPUT must end with .png.')

  return {
    targetURL,
    apiURL,
    output: isAbsolute(outputValue) ? outputValue : resolve(rootDirectory, outputValue),
    width: parseInteger('WIDTH', '1440', 320, 7680),
    height: parseInteger('HEIGHT', '900', 240, 4320),
    waitMilliseconds: parseInteger('WAIT_MS', '1000', 0, 60000),
    chromeBinary: cleanText('CHROME_BIN', 'google-chrome'),
    admin: targetURL.pathname === '/admin' || targetURL.pathname.startsWith('/admin/'),
  }
}

function isLoopback(hostname) {
  return ['localhost', '127.0.0.1', '[::1]'].includes(hostname.toLowerCase())
}

async function loadAdminPassword(apiURL) {
  if (!isLoopback(apiURL.hostname)) fail('API_URL must use a loopback host for admin capture.')

  if (Object.hasOwn(process.env, 'ADMIN_PASSWORD')) {
    if (!process.env.ADMIN_PASSWORD) fail('ADMIN_PASSWORD is unavailable.')
    return process.env.ADMIN_PASSWORD
  }

  let values
  try {
    values = parseEnv(await readFile(envFile, 'utf8'))
  } catch {
    fail('Root .env is unavailable or invalid.')
  }
  if (!values.ADMIN_PASSWORD) fail('ADMIN_PASSWORD is unavailable.')
  return values.ADMIN_PASSWORD
}

async function authenticate(config) {
  const password = await loadAdminPassword(config.apiURL)
  let response
  try {
    response = await fetch(new URL('/api/v1/admin/login', config.apiURL), {
      method: 'POST',
      headers: { 'content-type': 'application/json', origin: config.targetURL.origin },
      body: JSON.stringify({ password }),
      signal: AbortSignal.timeout(10000),
    })
  } catch {
    fail('Admin authentication server is unavailable.')
  }
  if (!response.ok) fail('Admin authentication failed.')

  return parseSessionCookie(response.headers.getSetCookie(), config.apiURL.protocol === 'https:')
}

function parseSessionCookie(setCookies, secure) {
  const encoded = setCookies.find((value) => value.startsWith(`${adminCookieName}=`))
  if (!encoded) fail('Admin authentication did not return a session.')

  const segments = encoded.split(';').map((segment) => segment.trim())
  const equals = segments[0].indexOf('=')
  if (equals < 1 || !segments[0].slice(equals + 1)) fail('Admin authentication returned an invalid session.')

  const attributes = new Map()
  for (const segment of segments.slice(1)) {
    const separator = segment.indexOf('=')
    const name = (separator < 0 ? segment : segment.slice(0, separator)).trim().toLowerCase()
    if (!name || attributes.has(name)) fail('Admin authentication returned an invalid session.')
    attributes.set(name, separator < 0 ? true : segment.slice(separator + 1).trim())
  }

  const expires = Date.parse(attributes.get('expires'))
  const maxAge = Number(attributes.get('max-age'))
  const expiresIn = expires - Date.now()
  const valid = segments[0].slice(0, equals) === adminCookieName
    && attributes.get('path') === adminCookiePath
    && !attributes.has('domain')
    && attributes.get('httponly') === true
    && String(attributes.get('samesite')).toLowerCase() === 'strict'
    && attributes.has('secure') === secure
    && Number.isInteger(maxAge)
    && maxAge === adminSessionSeconds
    && Number.isFinite(expires)
    && expiresIn > (adminSessionSeconds - 120) * 1000
    && expiresIn <= (adminSessionSeconds + 60) * 1000
  if (!valid) fail('Admin authentication returned an invalid session.')

  return {
    name: adminCookieName,
    value: segments[0].slice(equals + 1),
    path: adminCookiePath,
    httpOnly: true,
    sameSite: 'Strict',
    secure,
    expires: Math.floor(expires / 1000),
  }
}

function sessionCookieParameters(sessionCookie, apiURL) {
  return {
    ...sessionCookie,
    url: new URL(adminCookiePath, apiURL).href,
  }
}

async function validateOutput(output) {
  const parent = dirname(output)
  try {
    const info = await stat(parent)
    if (!info.isDirectory()) fail('OUTPUT parent must be a directory.')
    await access(parent, fsConstants.W_OK)
  } catch (error) {
    if (error instanceof CaptureError) throw error
    fail('OUTPUT parent directory is unavailable or not writable.')
  }
}

function printConfig(config) {
  console.log('[screenshot] capture-only; existing development servers are not managed')
  console.log(`[screenshot] URL=${JSON.stringify(config.targetURL.href)}`)
  console.log(`[screenshot] OUTPUT=${JSON.stringify(config.output)}`)
  console.log(`[screenshot] WIDTH=${config.width} HEIGHT=${config.height} WAIT_MS=${config.waitMilliseconds}`)
  console.log(`[screenshot] API_URL=${JSON.stringify(config.apiURL.href)} CHROME_BIN=${JSON.stringify(config.chromeBinary)}`)
}

async function launchChrome(binary) {
  profileDirectory = await mkdtemp(temporaryPrefix)
  const childEnvironment = {}
  for (const name of ['DBUS_SESSION_BUS_ADDRESS', 'DISPLAY', 'HOME', 'LANG', 'LC_ALL', 'PATH', 'XDG_RUNTIME_DIR']) {
    if (process.env[name]) childEnvironment[name] = process.env[name]
  }

  try {
    chrome = spawn(binary, [
      '--headless=new',
      '--no-sandbox',
      '--disable-gpu',
      '--disable-background-networking',
      '--no-default-browser-check',
      '--no-first-run',
      '--remote-debugging-address=127.0.0.1',
      '--remote-debugging-port=0',
      `--user-data-dir=${profileDirectory}`,
      'about:blank',
    ], { detached: true, env: childEnvironment, stdio: ['ignore', 'ignore', 'pipe'] })
  } catch {
    fail('Chrome could not be launched.')
  }

  return await new Promise((resolvePromise, rejectPromise) => {
    let stderr = ''
    const timeout = setTimeout(() => rejectPromise(new CaptureError('Chrome did not become ready.')), 10000)
    const done = (callback, value) => {
      clearTimeout(timeout)
      chrome.stderr.off('data', onData)
      chrome.off('error', onError)
      chrome.off('exit', onExit)
      callback(value)
    }
    const onData = (chunk) => {
      stderr = (stderr + chunk.toString('utf8')).slice(-8192)
      const match = stderr.match(/DevTools listening on (ws:\/\/[^\s]+)/u)
      if (match) done(resolvePromise, match[1])
    }
    const onError = () => done(rejectPromise, new CaptureError('Chrome could not be launched.'))
    const onExit = () => done(rejectPromise, new CaptureError('Chrome exited before capture.'))
    chrome.stderr.on('data', onData)
    chrome.once('error', onError)
    chrome.once('exit', onExit)
  })
}

class CDP {
  constructor(endpoint, WebSocketConstructor = globalThis.WebSocket) {
    this.nextId = 1
    this.pending = new Map()
    this.listeners = new Map()
    this.waiters = new Set()
    this.state = 'connecting'
    this.socket = new WebSocketConstructor(endpoint)
    this.opened = new Promise((resolvePromise, rejectPromise) => {
      this.resolveOpen = resolvePromise
      this.rejectOpen = rejectPromise
    })
    this.socket.addEventListener('open', () => {
      if (this.state !== 'connecting') return
      this.state = 'open'
      this.resolveOpen()
    }, { once: true })
    this.socket.addEventListener('message', (event) => this.onMessage(event.data))
    this.socket.addEventListener('error', () => this.failConnection(), { once: true })
    this.socket.addEventListener('close', () => this.failConnection(), { once: true })
  }

  async open() {
    if (this.state === 'open') return
    if (this.state === 'closed') fail('Chrome DevTools connection failed.')
    await this.opened
  }

  failConnection() {
    if (this.state === 'closed') return
    this.state = 'closed'
    const error = new CaptureError('Chrome DevTools connection closed.')
    this.rejectOpen(error)
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer)
      pending.reject(error)
    }
    this.pending.clear()
    for (const listeners of this.listeners.values()) {
      for (const listener of listeners) {
        clearTimeout(listener.timer)
        listener.reject(error)
      }
    }
    this.listeners.clear()
    for (const waiter of this.waiters) {
      clearTimeout(waiter.timer)
      waiter.reject(error)
    }
    this.waiters.clear()
  }

  onMessage(data) {
    let message
    try {
      message = JSON.parse(data)
    } catch {
      this.failConnection()
      try { this.socket.close() } catch {}
      return
    }
    if (message.id) {
      const pending = this.pending.get(message.id)
      if (!pending) return
      this.pending.delete(message.id)
      clearTimeout(pending.timer)
      if (message.error) pending.reject(new CaptureError('Chrome DevTools command failed.'))
      else pending.resolve(message.result)
      return
    }
    const key = `${message.sessionId ?? ''}:${message.method}`
    const callbacks = this.listeners.get(key) ?? []
    this.listeners.delete(key)
    for (const callback of callbacks) {
      clearTimeout(callback.timer)
      callback.resolve(message.params)
    }
  }

  send(method, params = {}, sessionId, timeoutMilliseconds = cdpCommandTimeoutMilliseconds) {
    if (this.state !== 'open') return Promise.reject(new CaptureError('Chrome DevTools connection is unavailable.'))
    const id = this.nextId++
    return new Promise((resolvePromise, rejectPromise) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        rejectPromise(new CaptureError('Chrome DevTools command timed out.'))
      }, timeoutMilliseconds)
      this.pending.set(id, { resolve: resolvePromise, reject: rejectPromise, timer })
      try {
        const message = { id, method, params }
        if (sessionId) message.sessionId = sessionId
        this.socket.send(JSON.stringify(message))
      } catch {
        clearTimeout(timer)
        this.pending.delete(id)
        rejectPromise(new CaptureError('Chrome DevTools connection is unavailable.'))
      }
    })
  }

  once(method, sessionId, timeoutMilliseconds = cdpCommandTimeoutMilliseconds) {
    if (this.state !== 'open') return Promise.reject(new CaptureError('Chrome DevTools connection is unavailable.'))
    const key = `${sessionId ?? ''}:${method}`
    return new Promise((resolvePromise, rejectPromise) => {
      const callbacks = this.listeners.get(key) ?? []
      const listener = { resolve: resolvePromise, reject: rejectPromise }
      listener.timer = setTimeout(() => {
        const current = this.listeners.get(key) ?? []
        const remaining = current.filter((candidate) => candidate !== listener)
        if (remaining.length) this.listeners.set(key, remaining)
        else this.listeners.delete(key)
        rejectPromise(new CaptureError('Chrome DevTools event timed out.'))
      }, timeoutMilliseconds)
      callbacks.push(listener)
      this.listeners.set(key, callbacks)
    })
  }

  wait(milliseconds) {
    if (this.state !== 'open') return Promise.reject(new CaptureError('Chrome DevTools connection is unavailable.'))
    return new Promise((resolvePromise, rejectPromise) => {
      const waiter = { reject: rejectPromise }
      waiter.timer = setTimeout(() => {
        this.waiters.delete(waiter)
        resolvePromise()
      }, milliseconds)
      this.waiters.add(waiter)
    })
  }

  close() {
    this.failConnection()
    try { this.socket.close() } catch {}
  }
}

async function withTimeout(promise, milliseconds, message) {
  let timer
  try {
    return await Promise.race([
      promise,
      new Promise((_, rejectPromise) => { timer = setTimeout(() => rejectPromise(new CaptureError(message)), milliseconds) }),
    ])
  } finally {
    clearTimeout(timer)
  }
}

async function capture(config, endpoint, sessionCookie) {
  const cdp = new CDP(endpoint)
  try {
    await withTimeout(cdp.open(), 5000, 'Chrome DevTools connection timed out.')
    const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank' })
    const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true })
    await cdp.send('Page.enable', {}, sessionId)
    await cdp.send('Network.enable', {}, sessionId)
    await cdp.send('Emulation.setDeviceMetricsOverride', {
      width: config.width,
      height: config.height,
      deviceScaleFactor: 1,
      mobile: false,
    }, sessionId)

    if (sessionCookie) {
      const result = await cdp.send('Network.setCookie', sessionCookieParameters(sessionCookie, config.apiURL), sessionId)
      if (!result.success) fail('Admin session could not be loaded into Chrome.')
    }

    const loaded = cdp.once('Page.loadEventFired', sessionId, 20000)
    loaded.catch(() => {})
    const navigation = await cdp.send('Page.navigate', { url: config.targetURL.href }, sessionId)
    if (navigation.errorText) fail('Target server is unavailable.')
    await loaded
    await cdp.wait(config.waitMilliseconds)

    const { contentSize } = await cdp.send('Page.getLayoutMetrics', {}, sessionId)
    const width = Math.max(1, Math.ceil(contentSize.width))
    const height = Math.max(1, Math.ceil(contentSize.height))
    const { data } = await cdp.send('Page.captureScreenshot', {
      format: 'png',
      fromSurface: true,
      captureBeyondViewport: true,
      clip: { x: 0, y: 0, width, height, scale: 1 },
    }, sessionId)
    partialOutput = join(dirname(config.output), `.${basename(config.output)}.${process.pid}.tmp`)
    await writeFile(partialOutput, Buffer.from(data, 'base64'), { mode: 0o600 })
    await rename(partialOutput, config.output)
    partialOutput = undefined
  } catch (error) {
    if (error instanceof CaptureError) throw error
    fail('Page capture failed.')
  } finally {
    cdp.close()
  }
}

function processGroupAlive(pid) {
  try {
    process.kill(-pid, 0)
    return true
  } catch (error) {
    if (error?.code === 'ESRCH') return false
    throw error
  }
}

async function waitForProcessGroupExit(child, timeoutMilliseconds) {
  const deadline = Date.now() + timeoutMilliseconds
  while (Date.now() < deadline) {
    if ((child.exitCode !== null || child.signalCode !== null) && !processGroupAlive(child.pid)) return true
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 25))
  }
  return (child.exitCode !== null || child.signalCode !== null) && !processGroupAlive(child.pid)
}

async function terminateProcessGroup(child, gracefulMilliseconds = 2000, forcedMilliseconds = 5000) {
  if (!child?.pid) return
  if ((child.exitCode !== null || child.signalCode !== null) && !processGroupAlive(child.pid)) return
  try { process.kill(-child.pid, 'SIGTERM') } catch (error) {
    if (error?.code !== 'ESRCH') throw error
  }
  if (await waitForProcessGroupExit(child, gracefulMilliseconds)) return

  try { process.kill(-child.pid, 'SIGKILL') } catch (error) {
    if (error?.code !== 'ESRCH') throw error
  }
  if (!await waitForProcessGroupExit(child, forcedMilliseconds)) {
    fail('Capture Chrome could not be terminated.')
  }
}

async function terminateChrome() {
  await terminateProcessGroup(chrome)
}

async function cleanup() {
  if (cleaningUp) return cleaningUp
  cleaningUp = (async () => {
    await terminateChrome()
    if (partialOutput) await rm(partialOutput, { force: true }).catch(() => {})
    if (profileDirectory) await rm(profileDirectory, { recursive: true, force: true }).catch(() => {})
  })()
  return cleaningUp
}

for (const [signal, exitCode] of [['SIGHUP', 129], ['SIGINT', 130], ['SIGTERM', 143]]) {
  process.once(signal, () => {
    cleanup()
      .then(() => process.exit(exitCode))
      .catch(() => {
        console.error('[screenshot] Capture Chrome cleanup failed.')
        process.exit(1)
      })
  })
}

async function main() {
  const config = parseConfig()
  await validateOutput(config.output)
  printConfig(config)
  const sessionCookie = config.admin ? await authenticate(config) : undefined
  const endpoint = await launchChrome(config.chromeBinary)
  await capture(config, endpoint, sessionCookie)
  console.log(`[screenshot] saved ${JSON.stringify(config.output)}`)
}

export { CDP, CaptureError, parseSessionCookie, sessionCookieParameters, terminateProcessGroup }

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  try {
    await main()
  } catch (error) {
    const message = error instanceof CaptureError ? error.message : 'Screenshot capture failed.'
    console.error(`[screenshot] ${message}`)
    process.exitCode = 1
  } finally {
    try {
      await cleanup()
    } catch {
      console.error('[screenshot] Capture Chrome cleanup failed.')
      process.exitCode = 1
    }
  }
}
