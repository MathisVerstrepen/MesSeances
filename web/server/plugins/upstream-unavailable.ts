import {
  markResponseUnavailable,
  upstreamFailureForEvent,
} from '../utils/upstreamAvailability'

export default defineNitroPlugin((nitroApp) => {
  nitroApp.hooks.hook('render:response', (response, { event }) => {
    const retryAfter = upstreamFailureForEvent(event)
    if (retryAfter === null) return
    markResponseUnavailable(
      response,
      response.statusCode ?? getResponseStatus(event),
      retryAfter,
    )
  })
})
