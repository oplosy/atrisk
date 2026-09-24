"""AtlasRisk deterministic risk-engine package skeleton."""

import platform

__version__ = "0.1.0"


def main() -> None:
    """Print the package diagnostic information."""
    print(diagnostics())


def diagnostics() -> str:
    """Return package and interpreter versions without network access."""
    return f"atlasrisk risk-engine version {__version__} runtime python {platform.python_version()}"
