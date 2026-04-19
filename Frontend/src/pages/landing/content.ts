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

export const HERO_CONTENT: HeroContent = {
  eyebrow: 'ContractPro',
  title: 'Проверка договоров с помощью ИИ за минуту',
  subtitle:
    'Анализируем риски по ГК РФ, подсвечиваем слабые формулировки и предлагаем рекомендации. Пояснения на простом языке — для юристов и бизнеса.',
  primaryCta: { label: 'Начать бесплатно', to: '/login' },
  secondaryCta: { label: 'Войти', to: '/login' },
  trustBadges: ['Соответствие 152-ФЗ', 'Хранение в РФ', 'До 20 МБ · 100 страниц PDF'],
};

export interface FeatureCard {
  id: string;
  icon: FeatureIconId;
  title: string;
  description: string;
}

export type FeatureIconId = 'scan' | 'risk' | 'recommend' | 'summary' | 'history' | 'shield';

export const FEATURES: FeatureCard[] = [
  {
    id: 'scan',
    icon: 'scan',
    title: 'Распознавание PDF',
    description:
      'Текстовые и сканированные PDF — OCR извлекает структуру и реквизиты автоматически.',
  },
  {
    id: 'risks',
    icon: 'risk',
    title: 'Карта рисков',
    description: 'Высокий, средний и низкий приоритет с ссылками на пункты договора и нормы ГК РФ.',
  },
  {
    id: 'recommendations',
    icon: 'recommend',
    title: 'Рекомендации по формулировкам',
    description: 'Улучшаем формулировки, чтобы снизить юридические и финансовые риски.',
  },
  {
    id: 'summary',
    icon: 'summary',
    title: 'Резюме на простом языке',
    description:
      'Краткий пересказ договора для бизнеса — ключевые условия без юридических терминов.',
  },
  {
    id: 'versions',
    icon: 'history',
    title: 'Сравнение версий',
    description:
      'Загружайте правки контрагента — подсветим различия в тексте и структуре документа.',
  },
  {
    id: 'security',
    icon: 'shield',
    title: 'Безопасность',
    description: 'Шифрование, хранение в РФ, RBAC и удаление по запросу — соответствует 152-ФЗ.',
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

export const PRICING_PLANS: PricingPlan[] = [
  {
    id: 'free',
    name: 'Бесплатный',
    price: '0 ₽',
    priceHint: 'навсегда',
    description: 'Познакомьтесь с платформой и проверьте 3 договора в месяц.',
    bullets: [
      'До 3 договоров в месяц',
      'Базовая карта рисков',
      'Резюме на простом языке',
      'Поддержка по e-mail',
    ],
    cta: { label: 'Начать бесплатно', to: '/login' },
  },
  {
    id: 'pro',
    name: 'Профи',
    price: '4 900 ₽',
    priceHint: 'в месяц',
    description: 'Для практикующих юристов и малого бизнеса.',
    bullets: [
      'До 100 договоров в месяц',
      'Рекомендации по формулировкам',
      'Сравнение версий',
      'Экспорт PDF и DOCX',
      'Приоритетная поддержка',
    ],
    cta: { label: 'Выбрать «Профи»', to: '/login' },
    featured: true,
    badge: 'Популярный',
  },
  {
    id: 'team',
    name: 'Команда',
    price: 'По запросу',
    priceHint: 'от 15 000 ₽/мес',
    description: 'Для юридических отделов и компаний с собственными регламентами.',
    bullets: [
      'Безлимитные проверки',
      'Собственные политики и чек-листы',
      'Роли и разграничение доступа',
      'SSO и журнал аудита',
      'Персональный менеджер',
    ],
    cta: { label: 'Связаться с нами', to: '/login' },
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
