import express from 'express';
import orderRoutes from './modules/orders/orders.routes.js';

const app = express();
app.use('/api/v1/orders', orderRoutes);
export default app;
