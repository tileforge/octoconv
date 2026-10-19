import { expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import App from './App';
it('opens the approved workspace as the sole interface with a useful empty state', () => {
  const html = renderToStaticMarkup(<App />);
  expect(html).toContain('Рабочее пространство');
  expect(html).not.toContain('Legacy');
  expect(html).toContain('Добавить файл');
  expect(html).toContain('Подключите API');
  expect(html).not.toContain('presentation.pdf');
});
