import { releaseBuildLock } from '../tools/playwright-build.mjs'

export default function globalTeardown() {
  releaseBuildLock()
}
