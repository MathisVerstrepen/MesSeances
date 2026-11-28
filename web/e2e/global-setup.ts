import {
  acquireBuildLock,
  buildFixture,
  reusableBuild,
} from '../tools/playwright-build.mjs'

// Builds the production fixture once; every worker serves it on its own port.
export default async function globalSetup() {
  acquireBuildLock()
  if (reusableBuild())
    console.log('[playwright] PLAYWRIGHT_REUSE_BUILD=1: reusing fixture build')
  else await buildFixture()
  process.env.PLAYWRIGHT_FIXTURE_PREBUILT = '1'
}
