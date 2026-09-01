import { initializeRequestId, REQUEST_ID_HEADER } from '../utils/internalApi'

export default defineEventHandler((event) => {
  const requestId = initializeRequestId(event)
  setResponseHeader(event, REQUEST_ID_HEADER, requestId)
})
