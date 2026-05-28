import { type ClassValue, clsx } from 'clsx';
import { extendTailwindMerge } from 'tailwind-merge';

/*
 * Расширение tailwind-merge нашими кастомными утилитами из tailwind.config.ts.
 *
 * По умолчанию twMerge не знает про наши custom fontSize/shadow/borderRadius
 * ключи (см. §8.2 high-architecture.md, ADR-FE-09). Без этого расширения
 * `text-15` классифицируется как color-утилита по эвристике "text-*", и
 * при объединении с `text-white` он перетирает цвет → текст падает в browser
 * default (чёрный). Регрессия была поймана через Chromatic после Этапа 3.1.
 *
 * При добавлении нового токена в tailwind.config.ts (fontSize/boxShadow/
 * borderRadius/spacing) — синхронно расширь соответствующую группу здесь.
 */
const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      'font-size': [{ text: ['11', '12', '13', '14', '15', '16', '20', '60'] }],
      shadow: [{ shadow: ['card'] }],
      rounded: [{ rounded: ['pill'] }],
    },
  },
});

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
