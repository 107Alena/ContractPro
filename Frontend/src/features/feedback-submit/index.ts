// Barrel: публичный API feature feedback-submit (§6.1, §16.2 high-architecture).
//
// Импортировать ТОЛЬКО этот путь (FSD-граница). Потребители — FeedbackBlock
// widget на ResultPage (FE-TASK-046).
export type { SubmitFeedbackOptions } from './api/submit-feedback';
export { submitFeedback, submitFeedbackEndpoint } from './api/submit-feedback';
export type { SubmitFeedbackInput, SubmitFeedbackResponse } from './model/types';
export type {
  UseFeedbackSubmitOptions,
  UseFeedbackSubmitResult,
} from './model/use-feedback-submit';
export { useFeedbackSubmit } from './model/use-feedback-submit';
