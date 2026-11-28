import {
  initializeClientIp,
  initializeRequestId,
  REQUEST_ID_HEADER,
} from '../utils/internalApi'

export default defineEventHandler((event) => {
  const requestId = initializeRequestId(event)
  initializeClientIp(event, useRuntimeConfig(event).clientIpHeader)
  setResponseHeader(event, REQUEST_ID_HEADER, requestId)
})
