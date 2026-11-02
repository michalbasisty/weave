import { Router } from 'express';
import { prisma } from '../../app.js';

const router = Router();
// @req REQ-ORD-1
router.get('/', async (_req, res) => {
  res.json(await prisma.order.findMany());
});
router.post('/', async (req, res) => {
  res.json(await prisma.order.create({ data: req.body }));
});
export default router;
