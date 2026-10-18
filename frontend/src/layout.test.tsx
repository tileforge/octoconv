import { expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import App from './App';
it('opens the approved workspace with a reachable legacy switch and a useful empty state', () => {
  const html = renderToStaticMarkup(<App />);
  expect(html).toContain('Рабочее пространство');
  expect(html).toContain('Legacy');
  expect(html).toContain('Добавить файл');
  expect(html).toContain('Подключите API');
  expect(html).not.toContain('presentation.pdf');
});
