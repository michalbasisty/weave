import { useEffect, useState } from 'react';
import api from '../services/api';

export default function Orders() {
  const [rows, setRows] = useState([]);
  const quotes = trpc.quotes.list.useQuery();
  useEffect(() => {
    api.get('/orders').then((r) => setRows(r.data));
  }, []);
  return <ul>{rows.length + (quotes.data?.length ?? 0)}</ul>;
}
