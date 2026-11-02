import { lazy } from 'react';
import { Routes, Route } from 'react-router-dom';

const Orders = lazy(() => import('./pages/Orders'));

export default function App() {
  return (
    <Routes>
      <Route path="/orders" element={<Orders />} />
    </Routes>
  );
}
