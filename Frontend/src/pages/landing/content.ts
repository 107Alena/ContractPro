// Статичный контент для LandingPage (FE-TASK-041). Вся копия маркетинга — здесь,
// структурированной TS-константой. Причина выбора TS-файла (а не i18n JSON):
//   1) Приложение RU-only на v1 — i18next добавляет indirection без выигрыша.
//   2) Фичи/тарифы — structured data (иконка + bullets + CTA), JSON-i18n для таких
//      структур неудобен; TS даёт типы и autocomplete.
//   3) Компоненты секций остаются «чистыми» — layout без inline-копии.

export interface HeroContent {
  eyebrow: string;
  title: string;
  subtitle: string;
  primaryCta: { label: string; to: string };
  secondaryCta: { label: string; to: string };
  trustBadges: string[];
}

// Figma node 12:2 (Hero Section). trustBadges перенесены в TrustStripContent
// — Figma вынесла их в отдельную секцию Trust Strip (13:2).
export const HERO_CONTENT: HeroContent = {
  eyebrow: 'AI-платформа для договорной работы',
  title: 'Проверяйте договоры быстрее и без рисков',
  subtitle:
    'ContractPro анализирует договоры в юрисдикции РФ, находит юридические и финансовые риски и даёт понятные рекомендации — за минуты, а не часы.',
  primaryCta: { label: 'Попробовать бесплатно', to: '/login' },
  secondaryCta: { label: 'Запросить демо', to: '/login' },
  trustBadges: [],
};

// TrustStrip — Figma node 13:2.
export interface TrustItem {
  id: string;
  emoji: string;
  title: string;
  description: string;
}

export const TRUST_ITEMS: TrustItem[] = [
  {
    id: 'jurisdiction',
    emoji: '🇷🇺',
    title: 'Юрисдикция РФ',
    description: 'Работа с договорами по российскому праву',
  },
  {
    id: 'confidential',
    emoji: '🛡',
    title: 'Конфиденциально',
    description: 'Безопасная обработка документов',
  },
  {
    id: 'speed',
    emoji: '⚡',
    title: 'Быстрый результат',
    description: 'Анализ договора за несколько минут',
  },
  {
    id: 'clarity',
    emoji: '💡',
    title: 'Понятно каждому',
    description: 'Результат без юридического жаргона',
  },
];

// Features — Figma node 14:2 (7 карточек 4+3). Emoji-иконки 1:1 с figma.
export interface FeatureCard {
  id: string;
  emoji: string;
  title: string;
  description: string;
}

export const FEATURES: FeatureCard[] = [
  {
    id: 'check',
    emoji: '🔍',
    title: 'Проверка договора',
    description:
      'Загрузите файл — получите полный анализ с разбором каждого пункта и оценкой соответствия нормам',
  },
  {
    id: 'risks',
    emoji: '⚠️',
    title: 'Выявление рисков',
    description:
      'Автоматическое обнаружение юридических и финансовых рисков с приоритизацией по степени важности',
  },
  {
    id: 'mandatory',
    emoji: '✅',
    title: 'Обязательные условия',
    description:
      'Контроль наличия всех обязательных пунктов для каждого типа договора по законодательству РФ',
  },
  {
    id: 'recommendations',
    emoji: '💬',
    title: 'Рекомендации',
    description:
      'Конкретные предложения по формулировкам: что исправить, что добавить, на что обратить внимание',
  },
  {
    id: 'summary',
    emoji: '📋',
    title: 'Краткое резюме',
    description:
      'Понятная выжимка из договора простым языком — суть, условия, стороны, сроки и суммы',
  },
  {
    id: 'versions',
    emoji: '🔄',
    title: 'Сравнение версий',
    description:
      'Наглядное сравнение двух версий документа с подсветкой всех изменений и их оценкой',
  },
  {
    id: 'export',
    emoji: '📤',
    title: 'Экспорт и шеринг',
    description:
      'Выгрузка отчёта в PDF или отправка защищённой ссылки коллегам для совместного обсуждения',
  },
];

// ForWhom — Figma node 16:2.
export interface AudienceCard {
  id: string;
  emoji: string;
  title: string;
  description: string;
  bullets: string[];
}

export const AUDIENCES: AudienceCard[] = [
  {
    id: 'sme',
    emoji: '🏢',
    title: 'Малый и средний бизнес',
    description:
      'Понятный результат проверки без постоянного участия юриста. Экономьте время и деньги.',
    bullets: [
      'Быстрая проверка перед подписанием',
      'Понятные рекомендации простым языком',
      'Доступные тарифы для МСП',
    ],
  },
  {
    id: 'legal',
    emoji: '⚖️',
    title: 'Юридические департаменты',
    description:
      'Ускорение первичного анализа и структурирование рисков. Больше времени на сложные задачи.',
    bullets: [
      'Автоматическая приоритизация рисков',
      'Проверка обязательных условий',
      'Единый формат отчётности',
    ],
  },
  {
    id: 'commerce',
    emoji: '📊',
    title: 'Закупки и продажи',
    description:
      'Быстрое понимание спорных условий и следующих шагов. Уверенность в каждом согласовании.',
    bullets: [
      'Понимание рисков без юриста',
      'Сравнение версий при согласовании',
      'Быстрая передача контекста коллегам',
    ],
  },
];

// Security — Figma node 18:2.
export interface SecurityFeature {
  id: string;
  emoji: string;
  title: string;
  description: string;
}

export const SECURITY_FEATURES: SecurityFeature[] = [
  {
    id: 'confidential',
    emoji: '🔒',
    title: 'Конфиденциальность',
    description: 'Документы обрабатываются в защищённой среде и не передаются третьим лицам',
  },
  {
    id: 'jurisdiction',
    emoji: '🇷🇺',
    title: 'Юрисдикция РФ',
    description: 'Анализ на основе действующего российского законодательства',
  },
  {
    id: 'transparency',
    emoji: '📊',
    title: 'Прозрачность',
    description: 'Каждая рекомендация сопровождается пояснением и ссылкой на источник',
  },
  {
    id: 'liability',
    emoji: '⚖️',
    title: 'Границы ответственности',
    description: 'Сервис выдаёт рекомендации, но не заменяет полноценное юридическое заключение',
  },
];

// Outcomes — Figma node 17:2.
export interface OutcomeMetric {
  id: string;
  metric: string;
  label: string;
}

export const OUTCOMES: OutcomeMetric[] = [
  { id: 'speed', metric: 'в 10×', label: 'быстрее проверка договора' },
  { id: 'risks', metric: '85%', label: 'рисков выявляются автоматически' },
  { id: 'time', metric: '−40%', label: 'меньше времени на согласование' },
  { id: 'format', metric: '1 формат', label: 'единый отчёт для всей команды' },
];

// HowItWorks — Figma node 15:2.
export interface HowItWorksStep {
  id: string;
  number: string;
  title: string;
  description: string;
}

export const HOW_IT_WORKS_STEPS: HowItWorksStep[] = [
  {
    id: 'upload',
    number: '01',
    title: 'Загрузите документ',
    description: 'Перетащите файл, вставьте текст или загрузите PDF',
  },
  {
    id: 'analyze',
    number: '02',
    title: 'AI-анализ',
    description: 'ContractPro определит тип договора и проведёт проверку по всем параметрам',
  },
  {
    id: 'result',
    number: '03',
    title: 'Результат',
    description: 'Получите список рисков, рекомендации по формулировкам и краткое резюме',
  },
  {
    id: 'act',
    number: '04',
    title: 'Действуйте',
    description: 'Скачайте отчёт, отправьте ссылку коллегам или сравните с другой версией',
  },
];

export interface PricingPlan {
  id: string;
  name: string;
  price: string;
  priceHint: string;
  description: string;
  bullets: string[];
  cta: { label: string; to: string };
  featured?: boolean;
  badge?: string;
}

// Pricing — Figma node 21:2 (3 тарифа: Free / Pro featured (DARK) / Plus).
export const PRICING_PLANS: PricingPlan[] = [
  {
    id: 'free',
    name: 'Free',
    price: 'Бесплатно',
    priceHint: '',
    description: 'Для знакомства с сервисом',
    bullets: [
      '3 проверки в месяц',
      'Базовый анализ рисков',
      'Краткое резюме',
      'PDF-экспорт',
      '1 пользователь',
    ],
    cta: { label: 'Начать бесплатно', to: '/login' },
  },
  {
    id: 'pro',
    name: 'Pro',
    price: '14 990 ₽',
    priceHint: '/ месяц',
    description: 'Для команд и регулярной работы',
    bullets: [
      'Безлимитные проверки',
      'Полный анализ рисков',
      'Рекомендации по формулировкам',
      'Сравнение версий',
      'Командный доступ до 10 чел.',
      'Приоритетная поддержка',
    ],
    cta: { label: 'Попробовать бесплатно', to: '/login' },
    featured: true,
  },
  {
    id: 'plus',
    name: 'Plus',
    price: '4 990 ₽',
    priceHint: '/ месяц',
    description: 'Для одного и/или нескольких пользователей',
    bullets: [
      'Расширенная AI-проверка договоров',
      'Ограниченный объём документов в месяц',
      '1-3 пользователя',
      'Рекомендации по формулировкам',
    ],
    cta: { label: 'Запросить демо', to: '/login' },
  },
];

export interface FaqItem {
  id: string;
  question: string;
  answer: string;
}

export const FAQ_ITEMS: FaqItem[] = [
  {
    id: 'formats',
    question: 'Какие форматы договоров поддерживаются?',
    answer:
      'В первой версии — только PDF (текстовые и сканированные) до 20 МБ и 100 страниц. Поддержка DOC/DOCX запланирована в следующих релизах.',
  },
  {
    id: 'accuracy',
    question: 'Насколько точен анализ?',
    answer:
      'ИИ ориентируется на нормы ГК РФ и типовые риски. Рекомендации — вспомогательные: окончательное решение остаётся за юристом. Для каждого риска мы показываем обоснование и ссылку на соответствующий пункт договора.',
  },
  {
    id: 'privacy',
    question: 'Что происходит с моими документами?',
    answer:
      'Документы хранятся в зашифрованном виде в российском облаке. Доступ — только у сотрудников вашей организации согласно ролям. Удаление по запросу или автоматически через настраиваемый срок.',
  },
  {
    id: 'team',
    question: 'Можно ли работать командой?',
    answer:
      'Да. В тарифе «Команда» есть роли (администратор, юрист, бизнес-пользователь), журнал аудита действий и собственные чек-листы проверки для вашей организации.',
  },
  {
    id: 'integration',
    question: 'Есть ли API и интеграции?',
    answer:
      'Публичное API запланировано после релиза v1. Если интеграция критична — напишите нам: рассмотрим приоритизацию в дорожной карте.',
  },
  {
    id: 'legal',
    question: 'Заменяет ли сервис юриста?',
    answer:
      'Нет. ContractPro — инструмент поддержки принятия решений, который ускоряет подготовку и помогает не пропустить типовые риски. Заключение договоров остаётся в зоне ответственности юридической команды.',
  },
];
