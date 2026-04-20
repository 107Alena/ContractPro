// Barrel: публичный API feature-слайса auth. Экспорты подключаются по мере
// появления соответствующих под-слайсов (login — FE-TASK-029; logout — тонкий
// React-хук useLogout, FE-TASK-033; refresh реализован в processes/auth-flow).
export {
  LOGIN_EMAIL_MAX,
  LOGIN_PASSWORD_MAX,
  LOGIN_PASSWORD_MIN,
  LoginForm,
  type LoginFormProps,
  type LoginFormValues,
  loginSchema,
} from './login';
export { useLogout, type UseLogoutResult } from './logout';
