export interface User {
  id: number
  username: string
  email: string
  status: string
  roles: string[]
}
export interface TokenPair {
  access_token: string
  refresh_token: string
  expires_in: number
}
