import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";

import { ApiRequestError } from "./api";
import { useApiResource } from "./useApiResource";

interface ProbeProps {
  load: (signal: AbortSignal) => Promise<{ value: string }>;
}

const alwaysNotEmpty = () => false;

function Probe({ load }: ProbeProps): ReactNode {
  const resource = useApiResource(load, alwaysNotEmpty);
  return (
    <>
      <span data-testid="state">{resource.state}</span>
      <span data-testid="value">{resource.data?.value ?? "none"}</span>
      <button type="button" onClick={resource.retry}>
        Retry
      </button>
    </>
  );
}

it("keeps the last good response visible as stale after a retry fails", async () => {
  const load = vi
    .fn<(signal: AbortSignal) => Promise<{ value: string }>>()
    .mockResolvedValueOnce({ value: "known" })
    .mockRejectedValueOnce(
      new ApiRequestError("server-error", "temporary failure", 503),
    );
  render(<Probe load={load} />);

  await waitFor(() =>
    expect(screen.getByTestId("state")).toHaveTextContent("ready"),
  );
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await waitFor(() =>
    expect(screen.getByTestId("state")).toHaveTextContent("stale"),
  );
  expect(screen.getByTestId("value")).toHaveTextContent("known");
});
