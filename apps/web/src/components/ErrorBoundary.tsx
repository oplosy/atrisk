import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(): State {
    return { hasError: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("AtlasRisk shell error", error, info.componentStack);
  }

  render() {
    if (this.state.hasError) {
      return (
        <main className="crash-state" aria-labelledby="crash-title">
          <p className="eyebrow">AtlasRisk / shell</p>
          <h1 id="crash-title">The shell needs a refresh</h1>
          <p>
            A view failed before it could render. Refresh to restore the
            decision-support workspace.
          </p>
          <button
            className="button"
            type="button"
            onClick={() => window.location.reload()}
          >
            Refresh workspace
          </button>
        </main>
      );
    }
    return this.props.children;
  }
}
