// Compile Vue components for the client renderer without requiring a browser.
export default {
  name: 'vue-renderer',
  transformMode: 'web',
  setup() { return { teardown() {} } },
}
