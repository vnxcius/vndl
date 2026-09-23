import { Outlet } from "@tanstack/react-router";
import { TooltipProvider } from "@/components/ui/tooltip";

export function RootLayout() {
  return (
    <TooltipProvider>
      <div className="min-h-svh bg-background text-foreground">
        <Outlet />
      </div>
    </TooltipProvider>
  );
}
