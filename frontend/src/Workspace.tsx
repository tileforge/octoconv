import { useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { ApiError, cancelJob, createJob, isTerminal, listFormats, listJobs } from './api';
import type { FormatsResponse, JobStatus, JobStatusResponse, JobSummary, JobsPage } from './api';
import { operationTable, operationsOf, sourceFormatOfFilename, targetFormatsForOperation } from './formats';
import { clearSubmittedFile, downloadsForJob, needsJobsRefresh, retainedJobSelection, requestGeneration, startJobsRefresh, watchJob } from './jobTracking';

const storageKey = 'octoconv.apiKey';
const statusNames: Record<JobStatus, string> = { awaiting_upload: 'Ожидает загрузки', queued: 'В очереди', active: 'В работе', done: 'Готово', failed: 'Ошибка', canceled: 'Отменена' };
const operationNames: Record<string, string> = { convert: 'Конвертация', sanitize: 'Очистка', render: 'Рендер', inspect: 'Проверка' };
function errorText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 401 || error.status === 403) return 'Нет доступа. Проверьте API-ключ в настройках подключения.';
    if (error.status === 413) return 'Файл превышает допустимый размер загрузки.';
    if (error.status === 429) return error.code === 'quota_exceeded' ? 'Дневная квота исчерпана. Повторите позже.' : 'Слишком много запросов. Попробуйте чуть позже.';
    if (error.status === 404) return 'Задача не найдена или больше недоступна.';
    if (error.status >= 500) return 'Сервис временно недоступен. Попробуйте ещё раз.';
    return error.message;
  }
  return 'Не удалось связаться с API. Проверьте подключение и повторите запрос.';
}
function savedKey() {
  try { return typeof window === 'undefined' ? '' : window.localStorage.getItem(storageKey) ?? ''; } catch { return ''; }
}
function dateLabel(value: string | null | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? '—' : date.toLocaleString('ru-RU', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' });
}
function bytes(value: number) { return new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 1 }).format(value / (value >= 1048576 ? 1048576 : 1024)) + (value >= 1048576 ? ' МБ' : ' КБ'); }
function Icon({ type }: { type: 'upload' | 'refresh' | 'settings' | 'arrow' | 'file' | 'shield' | 'download' }) {
  const paths = { upload: 'M12 16V3m-5 5 5-5 5 5M4 16v5h16v-5', refresh: 'M20 7v5h-5M4 17v-5h5M6 7a7 7 0 0 1 12-1l2 3M4 15l2 3a7 7 0 0 0 12-1', settings: 'M4 7h16M4 17h16M8 4v6M16 14v6', arrow: 'M5 12h14m-6-6 6 6-6 6', file: 'M14 2H5v20h14V7l-5-5Zm0 0v6h5', shield: 'm12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Zm-4 9 3 3 5-6', download: 'M12 3v13m-5-5 5 5 5-5M4 17v4h16v-4' };
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={paths[type]} /></svg>;
}
function Panel({ children, className = '' }: { children: ReactNode; className?: string }) { return <section className={`oc-panel ${className}`}>{children}</section>; }
function Status({ status, progress }: { status: JobStatus; progress?: number | null }) {
  return <span className={`oc-pill oc-status-${status}`}>{statusNames[status] ?? status}{status === 'active' && progress != null ? ` · ${Math.round(progress)}%` : ''}</span>;
}

export default function Workspace({ onLegacy }: { onLegacy: () => void }) {
  const [apiKey, setApiKey] = useState(savedKey);
  const [session, setSession] = useState(0);
  const [opts, setOpts] = useState('');
  // Remounting on every applied credential replacement drops tenant-specific state immediately,
  // including when a user later switches back to a previously used key.
  return <WorkspaceSession key={session} opts={opts} setOpts={setOpts} apiKey={apiKey} onLegacy={onLegacy} onKey={(key) => { setApiKey(key); setSession(value => value + 1); }} />;
}
function WorkspaceSession({ apiKey, onKey, onLegacy, opts, setOpts }: { apiKey: string; onKey: (key: string) => void; onLegacy: () => void; opts: string; setOpts: (value: string) => void }) {
  const [settings, setSettings] = useState(!apiKey);
  const [draftKey, setDraftKey] = useState(apiKey);
  const [remember, setRemember] = useState(() => Boolean(savedKey()));
  const [storageNotice, setStorageNotice] = useState('');
  const [file, setFile] = useState<File | null>(null);
  const [dragging, setDragging] = useState(false);
  const [formats, setFormats] = useState<FormatsResponse | null>(null);
  const [formatsError, setFormatsError] = useState('');
  const [formatVersion, setFormatVersion] = useState(0);
  const [showFormats, setShowFormats] = useState(false);
  const [operation, setOperation] = useState('convert');
  const [target, setTarget] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState('');
  const [names, setNames] = useState<Record<string, { name: string; operation: string; size: number }>>({});
  const [status, setStatus] = useState('');
  const [engine, setEngine] = useState('');
  const [cursors, setCursors] = useState<string[]>([]);
  const [page, setPage] = useState<JobsPage | null>(null);
  const [listError, setListError] = useState('');
  const [loading, setLoading] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const [job, setJob] = useState<JobStatusResponse | null>(null);
  const [jobError, setJobError] = useState('');
  const [canceling, setCanceling] = useState(false);
  const [detailVersion, setDetailVersion] = useState(0);
  const uploadInput = useRef<HTMLInputElement>(null);
  const alive = useRef(true);
  const listBusy = useRef(false);
  const listGuard = useRef(requestGeneration());
  const detailGuard = useRef(requestGeneration());
  const selectedRef = useRef(selected); selectedRef.current = selected;
  useEffect(() => { alive.current = true; return () => { alive.current = false; listGuard.current.invalidate(); detailGuard.current.invalidate(); }; }, []);

  useEffect(() => {
    if (!apiKey) return;
    let live = true;
    setFormats(null); setFormatsError('');
    listFormats({ apiKey }).then(value => { if (live) setFormats(value); }).catch(error => { if (live) setFormatsError(errorText(error)); });
    return () => { live = false; };
  }, [apiKey, formatVersion]);
  const rows = useMemo(() => formats ? operationTable(formats) : [], [formats]);
  const operations = useMemo(() => formats ? operationsOf(formats) : [], [formats]);
  const activeOperation = operations.includes(operation) ? operation : (operations.includes('convert') ? 'convert' : operations[0] ?? '');
  const targets = useMemo(() => !formats ? [] : file ? targetFormatsForOperation(formats, activeOperation, sourceFormatOfFilename(file.name)) : [...new Set(rows.filter(row => row.operation === activeOperation).map(row => row.to))].sort(), [formats, file, activeOperation, rows]);
  const activeTarget = targets.includes(target) ? target : targets[0] ?? '';
  const cursor = cursors[cursors.length - 1] ?? '';
  useEffect(() => {
    if (!apiKey) return;
    const current = listGuard.current.next();
    listBusy.current = true;
    setLoading(true); setListError('');
    listJobs({ apiKey, status, engine, cursor, limit: 20 }).then(value => { if (current()) setPage(value); }).catch(error => { if (current()) setListError(errorText(error)); }).finally(() => { if (current()) { listBusy.current = false; setLoading(false); } });
    return () => listGuard.current.invalidate();
  }, [apiKey, status, engine, cursor, refresh]);

  const autoRefresh = needsJobsRefresh(page?.jobs ?? []);
  useEffect(() => {
    if (!apiKey || showFormats || !autoRefresh) return;
    return startJobsRefresh({
      ready: () => alive.current && !listBusy.current && document.visibilityState === 'visible',
      refresh: () => setRefresh(value => value + 1),
    });
  }, [apiKey, showFormats, autoRefresh, status, engine, cursor]);

  useEffect(() => {
    setJob(null); setJobError(''); setCanceling(false);
    if (!selected) return;
    let ended = false;
    return watchJob({ apiKey, jobId: selected,
      onJob(value) {
        if (!alive.current || selectedRef.current !== selected) return;
        setJob(value); setJobError('');
        setPage(previous => previous ? { ...previous, jobs: previous.jobs.map(row => row.job_id === value.job_id ? { ...row, status: value.status, progress_pct: value.progress_pct } : row) } : previous);
        if (isTerminal(value.status) && !ended) { ended = true; setRefresh(value => value + 1); }
      }, onError(error) { if (alive.current && selectedRef.current === selected) setJobError(errorText(error)); },
    });
  }, [apiKey, selected, detailVersion]);

  function resetList() {
    listGuard.current.invalidate();
    setPage(null); setCursors([]); selectedRef.current = null; setSelected(null); setJob(null); setJobError(''); detailGuard.current.invalidate();
  }
  function chooseJob(id: string) { if (selectedRef.current === id) return; selectedRef.current = id; detailGuard.current.invalidate(); setJob(previous => retainedJobSelection(previous, id)); setJobError(''); setSelected(id); }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!apiKey || !file || !activeTarget || submitting) return;
    if (opts.trim()) {
      try { const parsed = JSON.parse(opts); if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error(); }
      catch { setSubmitError('Параметры должны быть JSON-объектом, например {"quality":85}.'); setSettings(true); return; }
    }
    setSubmitting(true); setSubmitError('');
    const submittedFile = file;
    const uploaded = { name: file.name, operation: activeOperation, size: file.size };
    const selectionAtStart = selectedRef.current;
    try {
      const result = await createJob({ apiKey, file, target: activeTarget, operation: activeOperation === 'convert' ? undefined : activeOperation, opts });
      if (!alive.current) return;
      setNames(previous => ({ ...previous, [result.job_id]: uploaded }));
      if (cursor) { listGuard.current.invalidate(); setPage(null); }
      setCursors([]); setRefresh(value => value + 1);
      // Creating a job must not overwrite a different selection made during upload.
      if (selectedRef.current === selectionAtStart) chooseJob(result.job_id);
      setFile(current => clearSubmittedFile(current, submittedFile));
      if (uploadInput.current?.files?.[0] === submittedFile) uploadInput.current.value = '';
    } catch (error) { if (alive.current) setSubmitError(errorText(error)); }
    finally { if (alive.current) setSubmitting(false); }
  }
  async function cancel() {
    if (!job || canceling || isTerminal(job.status)) return;
    const current = detailGuard.current.next();
    const id = job.job_id;
    setCanceling(true); setJobError('');
    try {
      await cancelJob({ apiKey, jobId: id });
      if (alive.current && current() && selectedRef.current === id) { setDetailVersion(value => value + 1); setRefresh(value => value + 1); }
    } catch (error) { if (alive.current && current()) setJobError(errorText(error)); }
    finally { if (alive.current && current()) setCanceling(false); }
  }
  function applySettings(event: FormEvent) {
    event.preventDefault();
    const key = draftKey.trim();
    try { if (remember && key) window.localStorage.setItem(storageKey, key); else window.localStorage.removeItem(storageKey); }
    catch { setStorageNotice('Браузер не разрешил сохранить ключ. Он действует только до закрытия страницы.'); }
    if (key !== apiKey) onKey(key); else setSettings(false);
  }
  const summary: JobSummary | undefined = page?.jobs.find(row => row.job_id === selected);
  const known = selected ? names[selected] : undefined;
  const shownStatus = job?.status ?? summary?.status;
  const engines = Object.keys(formats?.engines ?? {}).sort();
  const downloads = downloadsForJob(job);
  return <div className="workspace">
    <header className="oc-nav"><a href="#tasks" className="oc-logo" aria-label="OctoConv">octo<b>conv</b></a><nav aria-label="Основная навигация"><button className={!showFormats ? 'oc-nav-active' : ''} onClick={() => setShowFormats(false)}>Задачи</button><button className={showFormats ? 'oc-nav-active' : ''} onClick={() => setShowFormats(true)}>Форматы</button></nav><button className="oc-connection" onClick={() => setSettings(value => !value)}><span className={`oc-dot ${apiKey ? 'connected' : ''}`} />{apiKey ? 'Ключ API задан' : 'Подключить API'}</button><button onClick={onLegacy}>Legacy ↗</button></header>
    <main className="oc-main" id="tasks"><div className="oc-breadcrumb">Рабочее пространство / {showFormats ? 'Форматы' : 'Задачи'}</div><div className="oc-heading"><div><h1>Конвертация под контролем.</h1><p className="oc-subtitle">Запускайте новые задачи и возвращайтесь к результатам в одной таблице.</p></div><button onClick={() => setSettings(value => !value)} aria-expanded={settings}><Icon type="settings" /> Настройки</button></div>
    {settings && <Panel className="oc-settings"><form onSubmit={applySettings}><div><h2>Подключение и параметры</h2><p className="oc-muted">Ключ открывает доступ только к задачам вашего клиента.</p></div><div className="oc-settings-grid"><label>API-ключ<input type="password" autoComplete="off" value={draftKey} onChange={event => setDraftKey(event.target.value)} placeholder="Введите ключ доступа" /></label><label>Параметры конвертации · JSON<input value={opts} onChange={event => setOpts(event.target.value)} placeholder='{"quality":85}' /></label></div><div className="oc-settings-foot"><label className="oc-check"><input type="checkbox" checked={remember} onChange={event => setRemember(event.target.checked)} /> Сохранить ключ в этом браузере</label><button className="oc-primary" type="submit">Применить настройки</button></div>{storageNotice && <p className="oc-error" role="alert">{storageNotice}</p>}</form></Panel>}
    <Panel className="oc-upload-panel"><form className="oc-compactform" onSubmit={submit}><div className={`oc-uploadsmall ${dragging ? 'oc-dragging' : ''}`} onDragOver={event => { event.preventDefault(); setDragging(true); }} onDragLeave={() => setDragging(false)} onDrop={event => { event.preventDefault(); setDragging(false); const incoming = event.dataTransfer.files; if (incoming.length !== 1) { setSubmitError('Добавьте один файл для каждой задачи.'); return; } setFile(incoming[0]); setSubmitError(''); }}><Icon type="upload" /><div className="oc-uploadname"><strong>{file ? file.name : 'Добавить файл'}</strong><span className="oc-muted">{file ? bytes(file.size) : 'Перетащите или выберите'}</span></div><label className="oc-filepicker">Обзор<input ref={uploadInput} type="file" aria-label="Выбрать файл" onChange={event => { setFile(event.target.files?.[0] ?? null); setSubmitError(''); }} /></label></div><label>Операция<select value={activeOperation} disabled={!formats} onChange={event => setOperation(event.target.value)}>{!operations.length && <option value="">Нет данных</option>}{operations.map(op => <option key={op} value={op}>{operationNames[op] ?? op}</option>)}</select></label><label>В формат<select value={activeTarget} disabled={!targets.length} onChange={event => setTarget(event.target.value)}>{!targets.length && <option value="">{file && formats ? 'Не поддерживается' : 'Выберите формат'}</option>}{targets.map(format => <option key={format} value={format}>{format.toUpperCase()}</option>)}</select></label><button type="submit" className="oc-primary" disabled={!apiKey || !file || !activeTarget || submitting}>{submitting ? 'Загрузка…' : 'Создать задачу'}<Icon type="arrow" /></button></form>{submitError && <p className="oc-error oc-inset" role="alert">{submitError}</p>}{formatsError && <div className="oc-inline-error" role="alert">{formatsError}<button onClick={() => setFormatVersion(value => value + 1)}>Повторить загрузку форматов</button></div>}{file && formats && !targets.length && <p className="oc-inline-error">Для этого файла и операции нет поддерживаемых форматов.</p>}</Panel>
    {showFormats ? <Panel><div className="oc-toolbar"><h2>Поддерживаемые форматы</h2><span className="oc-muted">Возможности вашего API</span></div>{!formats ? <div className="oc-empty">{apiKey ? 'Загружаем форматы…' : 'Подключите API, чтобы увидеть доступные преобразования.'}</div> : <div className="oc-tablewrap"><table><thead><tr><th>Операция</th><th>Движок</th><th>Из формата</th><th>В формат</th></tr></thead><tbody>{rows.map(row => <tr key={`${row.operation}-${row.engine}-${row.from}-${row.to}`}><td>{operationNames[row.operation] ?? row.operation}</td><td>{row.engine}</td><td>{row.from.toUpperCase()}</td><td>{row.to.toUpperCase()}</td></tr>)}</tbody></table></div>}</Panel> : <div className="oc-layout"><Panel><div className="oc-toolbar"><h2>Задачи</h2><span className="oc-muted">Последние задачи</span><button onClick={() => setRefresh(value => value + 1)} disabled={!apiKey || loading} aria-label="Обновить задачи"><Icon type="refresh" /></button></div><div className="oc-filters"><label>Статус<select value={status} onChange={event => { resetList(); setStatus(event.target.value); }}><option value="">Все статусы</option>{Object.entries(statusNames).map(([value, name]) => <option key={value} value={value}>{name}</option>)}</select></label><label>Движок<select value={engine} onChange={event => { resetList(); setEngine(event.target.value); }}><option value="">Все движки</option>{engines.map(value => <option key={value} value={value}>{value}</option>)}</select></label></div>
    {!apiKey ? <div className="oc-empty"><Icon type="shield" /><h3>Подключите API</h3><p>Добавьте ключ доступа, чтобы загрузить задачи и начать конвертацию.</p><button onClick={() => setSettings(true)} className="oc-outline">Настроить подключение</button></div> : loading && !page ? <div className="oc-empty" role="status">Загружаем задачи…</div> : listError && !page ? <div className="oc-empty"><p className="oc-error" role="alert">{listError}</p><button onClick={() => setRefresh(value => value + 1)}>Повторить</button></div> : !page?.jobs.length ? <div className="oc-empty"><Icon type="file" /><h3>{status || engine ? 'Нет задач по этим фильтрам' : 'Здесь появятся ваши задачи'}</h3><p>{status || engine ? 'Выберите другой статус или движок.' : 'Добавьте первый файл выше. Его результат останется в списке.'}</p></div> : <div className="oc-tablewrap"><table aria-label="Задачи конвертации"><thead><tr><th>Задача</th><th>Движок</th><th>Статус</th><th>Создана</th></tr></thead><tbody>{page.jobs.map(row => <tr key={row.job_id} className={selected === row.job_id ? 'oc-selected' : ''}><td><button className="oc-rowselect" onClick={() => chooseJob(row.job_id)} aria-pressed={selected === row.job_id}><span className="oc-fileicon">{row.target_format?.toUpperCase() || 'FILE'}</span><span><strong>{names[row.job_id]?.name ?? `Задача ${row.job_id.slice(0, 8)}`}</strong><span className="oc-muted">{row.source_format?.toUpperCase() || '—'} → {row.target_format?.toUpperCase() || '—'}</span></span></button></td><td>{row.engine}</td><td><Status status={row.status} progress={row.progress_pct} />{row.status === 'active' && row.progress_pct != null && <progress value={row.progress_pct} max="100" aria-label="Прогресс задачи" />}</td><td className="oc-date">{dateLabel(row.created_at)}</td></tr>)}</tbody></table></div>}
    {page && listError && <div className="oc-inline-error" role="alert">{listError}<button onClick={() => setRefresh(value => value + 1)} disabled={loading}>Повторить</button></div>}
    <div className="oc-foot"><span role="status">{loading && page ? 'Обновляем задачи…' : page ? `${page.jobs.length} на странице · страница ${cursors.length + 1}` : 'До 20 задач на странице'}</span><div className="oc-pagination"><button disabled={!cursors.length || loading} onClick={() => { resetSelection(); listGuard.current.invalidate(); setPage(null); setCursors(previous => previous.slice(0, -1)); }}>Назад</button><button disabled={!page?.next_cursor || loading} onClick={() => { const next = page?.next_cursor; if (next) { resetSelection(); listGuard.current.invalidate(); setPage(null); setCursors(previous => [...previous, next]); } }}>Далее</button></div></div></Panel>
    <aside className="oc-stack"><Panel className="oc-pad"><div className="oc-minihead">Выбранная задача</div>{!selected ? <div className="oc-detail-empty"><Icon type="file" /><h2>Все детали рядом</h2><p className="oc-muted">Выберите задачу в таблице, чтобы увидеть прогресс и скачать результаты.</p></div> : <><h2 className="oc-detail-title">{known?.name ?? `Задача ${selected.slice(0, 8)}`}</h2><p className="oc-id">{selected}</p><div className="oc-filehero">{summary?.target_format?.toUpperCase() ?? 'FILE'}</div>{shownStatus && <Status status={shownStatus} progress={job?.progress_pct ?? summary?.progress_pct} />}{!job && !jobError && <p className="oc-muted" role="status">Загружаем подробности…</p>}{job && !isTerminal(job.status) && <div className="oc-liveprogress">{job.progress_pct != null ? <progress value={job.progress_pct} max="100" aria-label="Прогресс выбранной задачи" /> : <p className="oc-muted">{job.status === 'queued' ? 'Ожидает свободного воркера' : 'Выполняется обработка…'}</p>}{job.eta_seconds != null && <p className="oc-muted">Примерно {job.eta_seconds} сек. до завершения</p>}</div>}<div className="oc-rule" />{known && <div className="oc-meta"><span>Операция</span><span>{operationNames[known.operation] ?? known.operation}</span></div>}{summary && <><div className="oc-meta"><span>Движок</span><span>{summary.engine}</span></div><div className="oc-meta"><span>Создана</span><span>{dateLabel(summary.created_at)}</span></div>{summary.started_at && <div className="oc-meta"><span>Начата</span><span>{dateLabel(summary.started_at)}</span></div>}{summary.finished_at && <div className="oc-meta"><span>Завершена</span><span>{dateLabel(summary.finished_at)}</span></div>}</>}{job?.status === 'failed' && <p className="oc-error" role="alert">{job.error_message || job.error_code || 'Не удалось выполнить конвертацию.'}</p>}{job?.status === 'canceled' && <p className="oc-muted">Задача отменена. Вы можете загрузить файл снова.</p>}{downloads.map(output => <a key={output.key} className="oc-download" href={output.url} target="_blank" rel="noopener noreferrer"><Icon type="download" />{output.role ? `${output.role} · ${output.format?.toUpperCase()} · ${bytes(output.size ?? 0)}` : 'Скачать результат'}</a>)}{job?.status === 'done' && downloads.length === 0 && !jobError && <p className="oc-muted">Результат пока недоступен. Обновите подробности.</p>}{job && !isTerminal(job.status) && <button className="oc-full oc-outline" disabled={canceling} onClick={cancel}>{canceling ? 'Отмена…' : 'Отменить задачу'}</button>}{jobError && <p className="oc-error" role="alert">{jobError}</p>}<button className="oc-full" onClick={() => { detailGuard.current.invalidate(); setDetailVersion(value => value + 1); }}><Icon type="refresh" />Обновить подробности</button></>}</Panel><Panel className="oc-pad"><h2>Доступ к API</h2><p className="oc-muted oc-connection-text">{apiKey ? 'Ключ применяется только к вашим задачам.' : 'Добавьте ключ, чтобы начать работу.'}</p><button className="oc-full" onClick={() => setSettings(value => !value)}><Icon type="settings" />Параметры подключения</button></Panel></aside></div>}
    <p className="oc-notice"><Icon type="shield" /> Конвертация в вашей инфраструктуре</p></main>
  </div>;
  function resetSelection() { selectedRef.current = null; detailGuard.current.invalidate(); setSelected(null); setJob(null); setJobError(''); }
}
