import { writeFile } from 'node:fs/promises'

// Actual Vue controls, Nitro proxy, Go handlers, private filesystem and disposable
// PostgreSQL. Only Google's identity/picture provider is the backend test fixture.
export async function avatarScenario({
  tab,
  go,
  evaluate,
  until,
  click,
  fill,
  check,
  request,
  mail,
  googleRedirect,
  cdp,
  visual,
  run,
}) {
  const page = await tab()
  // Hold delivery of real HTTP responses, not mocked session/account values.
  // Synthetic focus order tests do not establish native OS picker ordering.
  await cdp.send(
    'Page.addScriptToEvaluateOnNewDocument',
    {
      source: `(() => {
    const original = window.fetch;
    window.__avatarGate = { holds: {}, seen: {} };
    window.fetch = async function(...args) {
      const response = await original.apply(this, args);
      const path = new URL(typeof args[0] === 'string' ? args[0] : args[0].url, location.origin).pathname;
      const gate = window.__avatarGate.holds[path];
      if (gate) {
        window.__avatarGate.seen[path] = true;
        await new Promise(resolve => { gate.release = resolve; });
        if (gate.identityMismatch) {
          const body = await response.json();
          body.account.username += '_mismatch';
          return new Response(JSON.stringify(body), {status:response.status, headers:response.headers});
        }
      }
      return response;
    };
  })();`,
    },
    page.sessionId,
  )
  const preview = `document.querySelector('img[alt="Photo de profil"]')`
  const ready = async () =>
    until(
      page,
      `!!document.getElementById('trigger-avatar') && !document.getElementById('trigger-avatar').disabled`,
      'Avatar controls ready',
    )
  const image = async () =>
    until(
      page,
      `${preview}?.complete && ${preview}?.naturalWidth === 256 && ${preview}?.naturalHeight === 256`,
      'Processed avatar visible',
    )
  const details = async () =>
    (await request(page, '/account', undefined, 'GET')).body
  const processed = async (label, alpha = false) => {
    const path = (await details()).avatar_url
    const result = await evaluate(
      page,
      `(async () => {
      const response = await fetch(${JSON.stringify(path)}, {cache:'no-store'});
      const blob = await response.blob();
      const bytes = new Uint8Array(await blob.arrayBuffer());
      const view = new DataView(bytes.buffer);
      const text = offset => String.fromCharCode(...bytes.slice(offset, offset + 4));
      const chunks = [];
      let offset = 12;
      while (offset + 8 <= bytes.length) {
        chunks.push(text(offset));
        const size = view.getUint32(offset + 4, true);
        offset += 8 + size + (size % 2);
      }
      const bitmap = await createImageBitmap(blob);
      const canvas = document.createElement('canvas'); canvas.width = 256; canvas.height = 256;
      const context = canvas.getContext('2d'); context.drawImage(bitmap, 0, 0);
      const alpha = (x, y) => context.getImageData(x, y, 1, 1).data[3];
      const result = {
        headers: response.ok && response.headers.get('content-type') === 'image/webp' && response.headers.get('cache-control')?.includes('no-store') && response.headers.get('content-disposition') === 'inline; filename="avatar.webp"' && response.headers.get('x-content-type-options') === 'nosniff' && response.headers.get('cross-origin-resource-policy') === 'same-origin' && Number(response.headers.get('content-length')) === bytes.length,
        framing: blob.size >= 20 && blob.size <= 524288 && text(0) === 'RIFF' && text(8) === 'WEBP' && view.getUint32(4, true) + 8 === bytes.length && offset === bytes.length,
        lossy: chunks.includes('VP8 ') && !chunks.some(chunk => ['VP8L','ANIM','ANMF','EXIF','ICCP','XMP '].includes(chunk)),
        dimensions: bitmap.width === 256 && bitmap.height === 256,
        alpha: [alpha(64,64), alpha(192,64), alpha(192,192)]
      };
      bitmap.close();
      return result;
    })()`,
    )
    check(
      result.headers && result.framing,
      `${label}: actual private WebP headers, bounded exact RIFF and no-store`,
    )
    check(
      result.lossy && result.dimensions,
      `${label}: decoded 256x256 lossy static WebP without metadata`,
    )
    if (alpha)
      check(
        result.alpha[0] === 0 &&
          Math.abs(result.alpha[1] - 128) <= 1 &&
          result.alpha[2] === 255,
        `${label}: transparent, partial and opaque alpha retained`,
      )
  }
  const uploadCount = () =>
    page.requests.filter(
      (item) =>
        item.method === 'POST' && item.path === '/api/v1/account/avatar',
    ).length
  const openPicker = async () =>
    evaluate(
      page,
      `(() => {
    const input = document.getElementById('avatar-file');
    const click = input.click;
    // Exercise the actual Vue trigger while replacing only the OS dialog.
    input.click = () => {};
    try { document.getElementById('trigger-avatar').click(); } finally { input.click = click; }
  })()`,
    )
  const select = async (
    type = 'image/png',
    invalid = false,
    oversized = false,
    order = '',
  ) => {
    await ready()
    await openPicker()
    const before = uploadCount()
    if (order) {
      await evaluate(
        page,
        `(() => {
        window.__avatarGate.holds = { '/api/v1/auth/session': {}, '/api/v1/account': {} };
        window.__avatarGate.seen = {};
        window.__avatarPickerInput = document.getElementById('avatar-file');
      })()`,
      )
    }
    const focus = async () => {
      await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
      await until(
        page,
        `window.__avatarGate.seen['/api/v1/auth/session']`,
        'Real session response held',
      )
      check(
        await evaluate(
          page,
          `!document.getElementById('avatar-file').disabled`,
        ),
        `${order}: native input stays enabled during soft validation`,
      )
    }
    if (order === 'focus-before-change') await focus()
    await evaluate(
      page,
      `(async () => {
      const canvas = document.createElement('canvas'); canvas.width = 32; canvas.height = 32;
      const context = canvas.getContext('2d'); context.fillStyle = '#991b1b'; context.fillRect(0,16,32,16); context.fillStyle = 'rgba(31,111,120,0.5)'; context.fillRect(16,0,16,16);
      const type = ${JSON.stringify(type)};
      const blob = ${oversized ? 'new Blob([new Uint8Array(5242881)], {type})' : invalid ? "new Blob(['not an image'], {type})" : 'await new Promise(resolve => canvas.toBlob(resolve, type))'};
      const file = new File([blob], 'synthetic.' + ({'image/png':'png', 'image/jpeg':'jpg', 'image/webp':'webp', 'image/svg+xml':'svg'}[type]), {type});
      const input = document.getElementById('avatar-file'); const transfer = new DataTransfer(); transfer.items.add(file); input.files = transfer.files; input.dispatchEvent(new Event('change', {bubbles:true}));
    })()`,
    )
    if (order === 'change-before-focus') await focus()
    if (order) {
      const held = async (phase) => {
        check(
          await evaluate(
            page,
            `(() => {
          const input = document.getElementById('avatar-file');
          const save = [...document.querySelectorAll('button')].find(el => el.textContent.trim() === 'Enregistrer');
          return input === window.__avatarPickerInput && !input.disabled && !!save && save.disabled && input.__vueParentComponent.setupState.selected?.name.startsWith('synthetic.');
        })()`,
          ),
          `${order}/${phase}: filename and Save retained, writes disabled`,
        )
        await evaluate(
          page,
          `(async () => {
          const state = document.getElementById('avatar-file').__vueParentComponent.setupState;
          await state.save(); await state.save(true);
        })()`,
        )
        check(
          uploadCount() === before,
          `${order}/${phase}: no POST from guarded programmatic Save`,
        )
      }
      await held('session')
      await evaluate(
        page,
        `window.__avatarGate.holds['/api/v1/auth/session'].release(); delete window.__avatarGate.holds['/api/v1/auth/session']`,
      )
      await until(
        page,
        `window.__avatarGate.seen['/api/v1/account']`,
        'Real details response held',
      )
      await held('details')
      await evaluate(
        page,
        `window.__avatarGate.holds['/api/v1/account'].release(); delete window.__avatarGate.holds['/api/v1/account']`,
      )
      await ready()
      check(
        uploadCount() === before &&
          (await evaluate(
            page,
            `document.getElementById('avatar-file').__vueParentComponent.setupState.selected?.name.startsWith('synthetic.')`,
          )),
        `${order}: settled validation keeps selection without auto-upload`,
      )
    }
  }
  const installProbe = async () =>
    evaluate(
      page,
      `(() => {
    window.__avatarProbe = {created:0, revoked:0, active:new Set(), header:document.querySelector('header')};
    const create = URL.createObjectURL, revoke = URL.revokeObjectURL;
    URL.createObjectURL = function(blob) { const value = create.call(URL, blob); window.__avatarProbe.created++; window.__avatarProbe.active.add(value); return value; };
    URL.revokeObjectURL = function(value) { window.__avatarProbe.revoked++; window.__avatarProbe.active.delete(value); return revoke.call(URL,value); };
  })()`,
    )
  const capturePicker = async () => {
    await openPicker()
    await evaluate(
      page,
      `(() => {
      const input = document.getElementById('avatar-file');
      window.__staleAvatarPicker = {input, state:input.__vueParentComponent.setupState};
    })()`,
    )
  }
  const rejectedPicker = async (label) =>
    check(
      await evaluate(
        page,
        `(() => {
    const {input, state} = window.__staleAvatarPicker;
    const cleared = state.selected === null && input.value === '';
    const transfer = new DataTransfer(); transfer.items.add(new File(['synthetic'], 'stale.png', {type:'image/png'})); input.files = transfer.files;
    state.choose({target:input});
    delete window.__staleAvatarPicker;
    return cleared && state.selected === null && input.value === '';
  })()`,
      ),
      label,
    )

  await go(page, '/connexion')
  await googleRedirect(page, { mode: 'login' }, 'verified', '/finaliser')
  await fill(page, 'account-username', `avatar_${run}`)
  await click(page, 'Confirmer mon nom')
  await ready()
  await image()
  check(
    (await details()).avatar_url === '/api/v1/account/avatar/1',
    'synthetic Google signup imports processed private photo',
  )
  await processed('synthetic Google import')
  check(
    !(await request(page, '/auth/session', undefined, 'GET')).body.account
      .avatar_url,
    'session DTO has no avatar field',
  )
  const initial = await evaluate(page, `${preview}.src`)
  check(
    initial.startsWith('blob:'),
    'preview uses local Blob, never Google URL or raw private route',
  )
  await installProbe()
  const documents = page.documents
  const theaters = page.requests.filter(
    (item) => item.path === '/api/v1/theaters',
  ).length

  for (const type of ['image/png', 'image/jpeg', 'image/webp']) {
    const before = uploadCount()
    const sessions = page.requests.filter(
      (item) => item.path === '/api/v1/auth/session',
    ).length
    const previous = (await details()).avatar_url
    const order =
      type === 'image/png'
        ? 'focus-before-change'
        : type === 'image/jpeg'
          ? 'change-before-focus'
          : ''
    await select(type, false, false, order)
    check(
      uploadCount() === before,
      `${type}: selection never writes or decodes original`,
    )
    await click(page, 'Enregistrer')
    await ready()
    await image()
    await until(
      page,
      `document.activeElement?.id === 'trigger-avatar'`,
      'Saved avatar restores focus',
    )
    check(
      page.requests.filter((item) => item.path === '/api/v1/auth/session')
        .length ===
        sessions + (order ? 2 : 1),
      'local avatar notification never self-refreshes session',
    )
    check(
      uploadCount() === before + 1 && (await details()).avatar_url !== previous,
      `${type}: explicit multipart upload stores processed 256 WebP once`,
    )
    await processed(type, type !== 'image/jpeg')
    check(
      await evaluate(
        page,
        `![...document.querySelectorAll('button')].some(el => el.textContent.trim() === 'Enregistrer') && document.getElementById('avatar-file').value === ''`,
      ),
      `${type}: successful upload clears File and native input`,
    )
    check(
      (await request(page, previous.replace('/api/v1', ''), undefined, 'GET'))
        .status === 404,
      `${type}: previous revision is inaccessible`,
    )
  }
  check(
    page.documents === documents &&
      page.requests.filter((item) => item.path === '/api/v1/theaters')
        .length === theaters &&
      (await evaluate(
        page,
        `window.__avatarProbe.header === document.querySelector('header')`,
      )),
    'upload keeps SPA document/header and theater initialization',
  )
  check(
    await evaluate(
      page,
      `window.__avatarProbe.active.size === 1 && window.__avatarProbe.revoked >= 2`,
    ),
    'replacement revokes old preview object URLs',
  )
  const uploaded = (await details()).avatar_url
  const anonymous = await tab()
  await go(anonymous, '/connexion')
  check(
    (
      await request(
        anonymous,
        uploaded.replace('/api/v1', ''),
        undefined,
        'GET',
      )
    ).status === 401,
    'anonymous browser cannot fetch private revision',
  )

  for (const [type, invalid, oversized, message] of [
    ['image/svg+xml', false, false, 'Choisissez une photo JPEG'],
    ['image/png', false, true, '5 Mio maximum'],
    ['image/png', true, false, 'Ce format est refusé'],
  ]) {
    const before = uploadCount()
    await select(type, invalid, oversized)
    if (invalid) await click(page, 'Enregistrer')
    await until(
      page,
      `document.querySelector('[role="alert"]')?.textContent.includes(${JSON.stringify(message)})`,
      'Avatar validation error shown',
    )
    check(
      uploadCount() === before + (invalid ? 1 : 0) &&
        (await details()).avatar_url === uploaded,
      `${type}/${invalid ? 'malformed' : oversized ? 'oversized' : 'unsupported'}: rejection preserves stored photo`,
    )
  }
  for (const [status, code, uncertain] of [
    [429, 'rate_limited', false],
    [503, 'avatar_busy', true],
    [409, 'avatar_changed', true],
    [503, 'accounts_unavailable', true],
  ]) {
    await select()
    const before = uploadCount()
    page.fault = { path: '/api/v1/account/avatar', status, code, delay: 300 }
    await click(page, 'Enregistrer')
    check(
      await evaluate(
        page,
        `document.getElementById('trigger-email').disabled && document.getElementById('trigger-avatar').disabled`,
      ),
      `${status}/${code}: page mutations locked during upload`,
    )
    check(
      await evaluate(
        page,
        `(() => {
      const input = document.getElementById('avatar-file'), state = input.__vueParentComponent.setupState, selected = state.selected;
      const transfer = new DataTransfer(); transfer.items.add(new File(['synthetic'], 'replacement.png', {type:'image/png'})); input.files = transfer.files;
      input.dispatchEvent(new Event('change', {bubbles:true}));
      return input.disabled && state.selected === selected;
    })()`,
      ),
      `${status}/${code}: pending upload blocks native input and rejects selection changes`,
    )
    await until(
      page,
      uncertain
        ? `[...document.querySelectorAll('[role="status"]')].some(el => el.textContent.includes('revérifié'))`
        : `document.querySelector('[role="alert"]')?.textContent.includes('Trop de tentatives')`,
      'Avatar safe failure recovery',
    )
    await ready()
    await image()
    check(
      uploadCount() === before + 1 && (await details()).avatar_url === uploaded,
      `${status}/${code}: no automatic write retry, old image preserved`,
    )
  }

  await cdp.send(
    'Emulation.setDeviceMetricsOverride',
    { width: 320, height: 850, deviceScaleFactor: 1, mobile: true },
    page.sessionId,
  )
  check(
    await evaluate(
      page,
      `document.documentElement.scrollWidth <= innerWidth && [...document.querySelectorAll('section[aria-labelledby="account-identity"] button')].filter(el => el.getClientRects().length).every(el => el.getBoundingClientRect().height >= 44)`,
    ),
    'Photo controls wrap at 320 px with 44 px targets',
  )
  if (visual) {
    const result = await cdp.send(
      'Page.captureScreenshot',
      { format: 'png', captureBeyondViewport: true },
      page.sessionId,
    )
    await writeFile(
      '/tmp/opencode/account-avatar-mobile.png',
      Buffer.from(result.data, 'base64'),
    )
  }
  await cdp.send('Emulation.clearDeviceMetricsOverride', {}, page.sessionId)
  await googleRedirect(page, { mode: 'login' }, 'verified', '/compte')
  await ready()
  await image()
  check(
    (await details()).avatar_url === uploaded,
    'later synthetic Google login never replaces uploaded photo',
  )
  await installProbe()
  const sibling = await tab(page.browserContextId)
  await go(sibling, '/compte')
  await until(
    sibling,
    `${preview}?.complete && ${preview}?.naturalWidth === 256`,
    'Same-owner second tab preview',
  )
  await click(page, 'Supprimer la photo')
  await ready()
  check(
    (await details()).avatar_url === null &&
      (await evaluate(
        page,
        `!${preview} && document.activeElement.id === 'trigger-avatar' && window.__avatarProbe.revoked >= 1`,
      )),
    'remove revokes preview, clears DB state and restores focus',
  )
  await until(
    sibling,
    `!!document.getElementById('trigger-avatar') && !${preview}`,
    'Other tab refreshes removed photo',
  )
  check(true, 'avatar notification refreshes other tabs without self-delivery')
  await googleRedirect(page, { mode: 'login' }, 'verified', '/compte')
  await ready()
  check(
    (await details()).avatar_url === null &&
      (await evaluate(page, `!${preview}`)),
    'explicit removal stays empty across synthetic Google login',
  )

  // Existing password account, untouched empty avatar, explicit Google association.
  const existing = await tab()
  await go(existing, '/inscription')
  const email = `avatar-existing-${run}@example.test`
  const password = 'Avatar synthetic password 42!'
  check(
    (await request(existing, '/auth/register', { email, password })).status ===
      202,
    'existing-account fixture registered',
  )
  const verification = await mail(email, 'verification')
  check(
    (
      await request(existing, '/auth/verification/confirm', {
        token: verification.token,
      })
    ).status === 200,
    'existing-account fixture verified',
  )
  check(
    (
      await request(existing, '/account/username', {
        username: `avatar_existing_${run}`,
      })
    ).status === 200,
    'existing-account fixture complete',
  )
  check(
    (await request(existing, '/account', undefined, 'GET')).body.avatar_url ===
      null,
    'existing password account starts untouched empty',
  )
  const proof = await request(existing, '/account/reauth/password', {
    password,
    action: 'google_link',
  })
  check(proof.status === 200, 'existing Google association authorized normally')
  await googleRedirect(
    existing,
    { mode: 'link', grant: proof.body.grant },
    'link',
    '/compte',
  )
  await until(
    existing,
    `${preview}?.complete && ${preview}?.naturalWidth === 256`,
    'Existing account imported preview',
  )
  check(
    (await request(existing, '/account', undefined, 'GET')).body.avatar_url ===
      '/api/v1/account/avatar/1',
    'explicit synthetic Google link fills untouched existing account',
  )

  // Route departure and lost-session cleanup use real reactive Vue lifecycle.
  await select()
  await click(page, 'Enregistrer')
  await ready()
  await image()
  await installProbe()
  await select()
  await capturePicker()
  await evaluate(
    page,
    `document.querySelector('#__nuxt').__vue_app__.config.globalProperties.$router.push('/films')`,
  )
  await until(page, `location.pathname === '/films'`, 'Avatar route departed')
  check(
    await evaluate(page, `window.__avatarProbe.revoked >= 1 && !${preview}`),
    'SPA route departure revokes private preview',
  )
  await rejectedPicker(
    'SPA departure clears selected File and rejects stale picker result',
  )
  await evaluate(
    page,
    `document.querySelector('#__nuxt').__vue_app__.config.globalProperties.$router.push('/compte')`,
  )
  await ready()
  await image()
  for (const invalidation of ['failure', 'identity mismatch']) {
    await select()
    await capturePicker()
    const before = uploadCount()
    if (invalidation === 'failure')
      page.fault = {
        path: '/api/v1/auth/session',
        status: 503,
        code: 'accounts_unavailable',
      }
    else
      await evaluate(
        page,
        `window.__avatarGate.holds = {'/api/v1/auth/session': {identityMismatch:true}}; window.__avatarGate.seen = {}`,
      )
    await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
    if (invalidation !== 'failure') {
      await until(
        page,
        `window.__avatarGate.seen['/api/v1/auth/session']`,
        'Session mismatch response held',
      )
      await evaluate(
        page,
        `window.__avatarGate.holds['/api/v1/auth/session'].release(); delete window.__avatarGate.holds['/api/v1/auth/session']`,
      )
    }
    await until(
      page,
      `!document.getElementById('trigger-avatar')`,
      'Invalidated picker controls removed',
    )
    await rejectedPicker(
      `${invalidation}: draft cleared and stale picker rejected`,
    )
    check(uploadCount() === before, `${invalidation}: no avatar write`)
    await go(page, '/compte')
    await ready()
    await image()
  }
  await installProbe()
  await select()
  await capturePicker()
  check(
    (await request(page, '/auth/logout', {})).status === 204,
    'real session revoked outside visible settings',
  )
  await evaluate(page, `window.dispatchEvent(new Event('focus'))`)
  await until(
    page,
    `!document.getElementById('trigger-avatar')`,
    'Lost session clears photo controls',
  )
  check(
    await evaluate(page, `window.__avatarProbe.revoked >= 1 && !${preview}`),
    'lost session releases selected File and processed object URL',
  )
  await rejectedPicker('logout rejects result from previously open picker')
}
