import platform

from atlasrisk import __version__, diagnostics


def test_package_version() -> None:
    assert __version__ == "0.1.0"


def test_diagnostics_report_runtime() -> None:
    assert diagnostics() == (
        f"atlasrisk risk-engine version 0.1.0 runtime python {platform.python_version()}"
    )
