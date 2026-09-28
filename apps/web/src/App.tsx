import { useEffect, useState } from "react";

import { ErrorBoundary } from "./components/ErrorBoundary";
import { RootRoute, type ShellRoute } from "./routes/root/RootRoute";

function routeFromLocation(): ShellRoute {
  const path = window.location.pathname as ShellRoute;
  return [
    "/",
    "/timeline",
    "/portfolio",
    "/risk",
    "/journal",
    "/settings",
  ].includes(path)
    ? path
    : "/";
}

export function App() {
  const [route, setRoute] = useState<ShellRoute>(routeFromLocation);

  useEffect(() => {
    const handlePopState = () => setRoute(routeFromLocation());
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  const navigate = (nextRoute: ShellRoute) => {
    window.history.pushState({}, "", nextRoute);
    setRoute(nextRoute);
  };

  return (
    <ErrorBoundary>
      <RootRoute route={route} onNavigate={navigate} />
    </ErrorBoundary>
  );
}
