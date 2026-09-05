import { RecoveryCaseDetailClient } from "./RecoveryCaseDetailClient";

// Server component wrapper: Next.js 15+ passes dynamic route params as a
// Promise even to a page that immediately hands off to a client
// component for the actual data-fetching/rendering.
export default async function RecoveryCaseDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <RecoveryCaseDetailClient id={id} />;
}
