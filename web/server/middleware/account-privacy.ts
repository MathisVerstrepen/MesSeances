import {
  accountPrivacyHeaders,
  isAccountPrivatePath,
} from '../../shared/accountPrivacy'

export default defineEventHandler((event) => {
  if (isAccountPrivatePath(getRequestURL(event).pathname)) {
    setResponseHeaders(event, accountPrivacyHeaders)
  }
})
