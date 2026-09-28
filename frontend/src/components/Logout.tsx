import LogoutIcon from '@mui/icons-material/Logout'
import { ListItemButton, ListItemIcon, ListItemText } from '@mui/material'
import { useAtomValue } from 'jotai'
import { useNavigate } from 'react-router-dom'
import { serverURL } from '../atoms/settings'
import { useI18n } from '../hooks/useI18n'

export default function Logout() {
  const navigate = useNavigate()
  const url = useAtomValue(serverURL)

  const logout = async () => {
    localStorage.removeItem('token')
    await fetch(`${url}/auth/openid/logout`, { credentials: 'include' }).catch(() => undefined)
    navigate('/login')
    window.location.reload()
  }

  const { i18n } = useI18n()

  return (
    <ListItemButton onClick={logout}>
      <ListItemIcon>
        <LogoutIcon />
      </ListItemIcon>
      <ListItemText primary={i18n.t('rpcAuthenticationLabel')} />
    </ListItemButton>
  )
}
