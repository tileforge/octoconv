import { useState } from 'react';
import Legacy from './Legacy';
import Workspace from './Workspace';
import './legacy.css';
export { describeError } from './Legacy';

export default function App() {
  const [legacy, setLegacy] = useState(false);
  if (legacy) return <div className="legacy" lang="en"><div className="legacy-navigation"><button onClick={() => setLegacy(false)}>← Рабочее пространство</button><span>Legacy · прежний интерфейс</span></div><Legacy /></div>;
  return <Workspace onLegacy={() => setLegacy(true)} />;
}
