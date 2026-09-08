const authStorageKeys = [
  'cmdb_token',
  'cmdb_user_id',
  'cmdb_username',
  'cmdb_display_name',
  'cmdb_roles',
]

export function clearAuthStorage() {
  authStorageKeys.forEach(key => localStorage.removeItem(key))
}
