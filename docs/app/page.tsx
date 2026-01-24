import Link from 'next/link';

export default function HomePage() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center p-24">
      <div className="text-center">
        <h1 className="mb-4 text-4xl font-bold">ZAP Go</h1>
        <p className="mb-8 text-lg text-fd-muted-foreground">
          Go bindings for ZAP - Zero-Copy App Proto for AI agent communication
        </p>
        <Link
          href="/docs"
          className="rounded-lg bg-fd-primary px-6 py-3 text-fd-primary-foreground hover:bg-fd-primary/90"
        >
          Read Documentation
        </Link>
      </div>
    </main>
  );
}
