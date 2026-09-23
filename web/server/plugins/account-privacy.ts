import {
  accountFragmentBootstrap,
  isAccountPage,
} from '../../shared/accountPrivacy'

export default defineNitroPlugin((nitroApp) => {
  nitroApp.hooks.hook('render:html', (html, { event }) => {
    if (isAccountPage(getRequestURL(event).pathname)) {
      html.head.unshift(`<script>${accountFragmentBootstrap}</script>`)
    }
  })
})
