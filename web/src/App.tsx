import { Loader } from "@mantine/core";
import { useQuery } from "@tanstack/react-query";
import { lazy, Suspense } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { api, call, type Library } from "./api/client";
import { Shell } from "./components/Shell";
import { enc } from "./lib/library";
import { Changes } from "./pages/Changes";
import { Channels } from "./pages/Channels";
import { ImportPage } from "./pages/Import";
import { Lists } from "./pages/Lists";
import { RadioPage } from "./pages/Radio";
import { Radios } from "./pages/Radios";

// The map brings Leaflet, so it loads when first opened.
const MapPage = lazy(() => import("./pages/Map").then((m) => ({ default: m.MapPage })));

export function App() {
  return (
    <Routes>
      <Route path="/" element={<Home />} />
      <Route path="/l/:lib" element={<Shell />}>
        <Route index element={<Navigate to="channels" replace />} />
        <Route path="channels" element={<Channels />} />
        <Route
          path="map"
          element={
            <Suspense fallback={<Loader />}>
              <MapPage />
            </Suspense>
          }
        />
        <Route path="radios" element={<Radios />} />
        <Route path="radios/:radio" element={<RadioPage />} />
        <Route path="import" element={<ImportPage />} />
        <Route path="lists" element={<Lists />} />
        <Route path="changes" element={<Changes />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

/** Home opens the default library, whatever it's named. */
function Home() {
  const libraries = useQuery({
    queryKey: ["libraries"],
    queryFn: () => call<{ libraries: Library[] }>(api.GET("/libraries")),
  });
  const def = libraries.data?.libraries.find((l) => l.default);
  if (def) return <Navigate to={`/l/${enc(def.name)}/channels`} replace />;
  return <Loader m="xl" />;
}
