// useOnlineStatus — браузерная онлайн/офлайн-индикация по событиям window.
// Архитектура: §9.3 (sticky-баннер в Topbar), §9.4 (graceful degradation).
//
// Возвращает true/false для текущего состояния navigator.onLine.
// SSR-safe: при отсутствии window возвращает true (optimistic default —
// серверный рендер не видит сетевого состояния клиента).
//
// navigator.onLine не сверхнадёжен (лишь отражает наличие активной сетевой
// интерфейса ОС, не реальную доступность бэкенда), поэтому хук дополняет
// реактивный слой: события online/offline — единственный надёжный триггер
// ре-рендера.
import { useEffect, useState } from 'react';

function readOnline(): boolean {
  if (typeof navigator === 'undefined') return true;
  return navigator.onLine !== false;
}

export function useOnlineStatus(): boolean {
  const [online, setOnline] = useState<boolean>(readOnline);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const onOnline = (): void => setOnline(true);
    const onOffline = (): void => setOnline(false);
    window.addEventListener('online', onOnline);
    window.addEventListener('offline', onOffline);
    // При маунте синхронизируемся с текущим значением (его могла сменить
    // другая вкладка до того, как компонент успел подписаться).
    setOnline(readOnline());
    return (): void => {
      window.removeEventListener('online', onOnline);
      window.removeEventListener('offline', onOffline);
    };
  }, []);

  return online;
}
