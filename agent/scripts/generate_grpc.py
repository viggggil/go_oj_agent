"""从仓库 Proto 生成 Agent 使用的 Python gRPC bindings。

生成文件位于 app/grpcgen，不能手工修改；脚本只修正 Python 包前缀，使它们能
作为 Agent 应用包的一部分导入。
"""

import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
AGENT = ROOT / "agent"
OUT = AGENT / "app" / "grpcgen"
PROTOC = [sys.executable, "-m", "grpc_tools.protoc"]
PROTOS = [
    ROOT / "api" / "common" / "v1" / "common.proto",
    ROOT / "api" / "problem" / "v1" / "problem.proto",
    ROOT / "api" / "submission" / "v1" / "submission.proto",
]


def main() -> None:
    validate_root = subprocess.check_output(
        [
            "go",
            "list",
            "-m",
            "-f",
            "{{.Dir}}",
            "github.com/envoyproxy/protoc-gen-validate",
        ],
        text=True,
        cwd=ROOT,
    ).strip()
    validate_proto = Path(validate_root) / "validate"
    if not validate_proto.is_dir():
        raise RuntimeError(
            "protoc-gen-validate is not available; run `go mod download`"
        )
    command = [
        *PROTOC,
        "-I",
        str(ROOT),
        "-I",
        str(validate_proto.parent),
        "--python_out",
        str(OUT),
        "--grpc_python_out",
        str(OUT),
        *(str(proto.relative_to(ROOT)) for proto in PROTOS),
    ]
    subprocess.run(command, cwd=ROOT, check=True)
    validate_command = [
        *PROTOC,
        "-I",
        str(validate_proto.parent),
        "--python_out",
        str(OUT),
        str(validate_proto / "validate.proto"),
    ]
    subprocess.run(validate_command, cwd=ROOT, check=True)
    for path in OUT.rglob("*.py"):
        text = path.read_text(encoding="utf-8")
        text = re.sub(r"from api\.", "from app.grpcgen.api.", text)
        text = re.sub(r"from validate import", "from app.grpcgen.validate import", text)
        path.write_text(text, encoding="utf-8")
    for directory in [path for path in OUT.rglob("*") if path.is_dir()]:
        (directory / "__init__.py").touch()


if __name__ == "__main__":
    main()
