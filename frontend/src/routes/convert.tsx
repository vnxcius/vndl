import { createFileRoute } from "@tanstack/react-router";
import { type ConvertKind, ConvertPage } from "@/components/convert-page";

export const Route = createFileRoute("/convert")({
  validateSearch: (search: Record<string, unknown>): { type?: ConvertKind } =>
    search.type === "video" ? { type: "video" } : {},
  component: function Convert() {
    const { type } = Route.useSearch();
    return <ConvertPage kind={type ?? "image"} />;
  },
});
