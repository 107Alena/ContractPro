// ShareableMaterials — секция «Что доступно для шаринга» на странице «Отчёты»
// (Figma 231:2). Чисто описательный static-контент: перечисляет форматы
// материалов, которыми можно поделиться, и для кого они полезны.
//
// data-honesty: данные не нужны — это объяснение возможностей продукта, а не
// отчётные показатели. Ничего не выдумываем (share-агрегаты/счётчики ссылок —
// отдельный backend, опущены, см. figma-mapping Reports «Honesty-отклонения»).
const MATERIALS = [
  {
    icon: '📋',
    title: 'Краткая выжимка',
    description: 'Ключевые условия, риски и рекомендации в сжатом виде',
    audience: 'Для бизнеса и быстрого согласования',
  },
  {
    icon: '📄',
    title: 'Полный отчёт проверки',
    description: 'Детальный анализ всех разделов договора с рисками и рекомендациями',
    audience: 'Для юриста и детальной проверки',
  },
  {
    icon: '⇄',
    title: 'Отчёт по различиям',
    description: 'Подробное сравнение двух версий договора с акцентом на изменения',
    audience: 'Для обсуждения правок между сторонами',
  },
  {
    icon: '🔗',
    title: 'Защищённая ссылка',
    description: 'Доступ к результату без пересылки файла, с ограничением по сроку',
    audience: 'Для удобного доступа коллег и партнёров',
  },
] as const;

export function ShareableMaterials(): JSX.Element {
  return (
    <section
      aria-label="Что доступно для шаринга"
      data-testid="shareable-materials"
      className="flex flex-col gap-4 rounded-xl border border-border-subtle bg-bg p-4"
    >
      <h2 className="text-18 font-semibold text-fg">Что доступно для шаринга</h2>
      <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {MATERIALS.map((material) => (
          <li
            key={material.title}
            className="flex flex-col gap-2 rounded-lg border border-border-subtle bg-bg-muted p-4"
          >
            <span aria-hidden="true" className="text-20 leading-none">
              {material.icon}
            </span>
            <h3 className="text-14 font-semibold text-fg">{material.title}</h3>
            <p className="text-12 leading-[18px] text-fg-muted">{material.description}</p>
            <p className="text-11 font-medium text-brand-500">{material.audience}</p>
          </li>
        ))}
      </ul>
    </section>
  );
}
