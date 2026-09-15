#!/usr/bin/env python3
"""将领域 YAML 的外部引用集中到单文件，生成器与离线工具共享同一契约。"""
import argparse
import pathlib
import yaml

parser = argparse.ArgumentParser(description='生成独立的 CMDB OpenAPI 契约')
parser.add_argument('--output', type=pathlib.Path, help='自定义输出路径，用于生成漂移检查')
args = parser.parse_args()

ROOT = pathlib.Path(__file__).resolve().parent
spec = yaml.safe_load((ROOT / 'openapi.yaml').read_text())

def internalize(value):
    if isinstance(value, dict):
        return {key: (item.removeprefix('../openapi.yaml') if key == '$ref' else internalize(item)) for key, item in value.items()}
    if isinstance(value, list):
        return [internalize(item) for item in value]
    return value

for name, reference in spec['components']['schemas'].items():
    filename, fragment = reference['$ref'].split('#/')
    schema = yaml.safe_load((ROOT / filename).read_text())[fragment]
    spec['components']['schemas'][name] = internalize(schema)
(args.output or ROOT / 'openapi.bundled.yaml').write_text('# 自动生成；请修改 openapi.yaml 与 schemas/ 后运行生成命令。\n' + yaml.safe_dump(spec, allow_unicode=True, sort_keys=False))
