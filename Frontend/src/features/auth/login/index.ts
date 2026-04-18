// Barrel: публичный API feature auth/login (FE-TASK-029).
// Потребитель — pages/auth/LoginPage. Внутренние слои (model/ui) наружу не
// раскрываются; феатур-граница чёткая.
export {
  LOGIN_EMAIL_MAX,
  LOGIN_PASSWORD_MAX,
  LOGIN_PASSWORD_MIN,
  type LoginFormValues,
  loginSchema,
} from './model/schema';
export { LoginForm, type LoginFormProps } from './ui/LoginForm';
