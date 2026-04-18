// Barrel: публичный API feature-слайса auth. Экспорты подключаются по мере
// появления соответствующих под-слайсов (login — FE-TASK-029; logout/refresh
// реализованы в processes/auth-flow, здесь только заготовки под feature-level UI).
export {
  LOGIN_EMAIL_MAX,
  LOGIN_PASSWORD_MAX,
  LOGIN_PASSWORD_MIN,
  LoginForm,
  type LoginFormProps,
  type LoginFormValues,
  loginSchema,
} from './login';
