import { NextResponse } from 'next/server';

// Quotes by slug: read one, or create one.
export async function GET(_req: Request) {
  return NextResponse.json({ ok: true });
}

export async function POST() {
  return NextResponse.json({}, { status: 201 });
}
