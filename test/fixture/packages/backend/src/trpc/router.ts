import { initTRPC } from '@trpc/server';

const t = initTRPC.create();
const publicProcedure = t.procedure;

export const appRouter = t.router({
  quotes: t.router({
    list: publicProcedure.query(() => []),
    add: publicProcedure.mutation(() => ({})),
  }),
  ping: publicProcedure.query(() => 'pong'),
});

export type AppRouter = typeof appRouter;
