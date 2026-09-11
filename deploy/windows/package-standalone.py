"""Assemble explicitly supplied Windows standalone release materials."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import sys
import zipfile


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def inventory(path):
    if not path.exists():
        raise ValueError(f'missing input: {path}')
    files = [path] if path.is_file() else sorted(path.rglob('*'))
    for item in [path, *files]:
        if item.is_symlink() or (hasattr(item, 'is_junction') and item.is_junction()):
            raise ValueError(f'symlink input rejected: {item}')
    return [{'path': p.name if path.is_file() else p.relative_to(path).as_posix(),
             'sha256': digest(p)} for p in files if p.is_file()]


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('launcher', 'backend', 'web-dir', 'resource-dir', 'redis-dir',
                 'media-dir', 'licenses-dir', 'sources-dir', 'output-dir', 'output-zip'):
        parser.add_argument('--' + name, required=True, type=Path)
    for name in ('version', 'source-commit', 'zlm-revision', 'redis-revision', 'license-owner'):
        parser.add_argument('--' + name, required=True)
    args = parser.parse_args()
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]{0,79}', args.version) or args.version.endswith('.') or args.version.upper().split('.')[0] in {'CON', 'PRN', 'AUX', 'NUL', *('COM'+str(i) for i in range(1,10)), *('LPT'+str(i) for i in range(1,10))}:
        raise ValueError('invalid version')
    for label, value in [('source commit', args.source_commit), ('zlm revision', args.zlm_revision), ('redis revision', args.redis_revision)]:
        if not re.fullmatch(r'[0-9a-f]{40}', value):
            raise ValueError('invalid ' + label)
    if args.output_dir.exists():
        raise ValueError('output directory already exists')
    if args.output_zip.exists():
        raise ValueError('output ZIP already exists')
    inputs = {name: getattr(args, name if name in ('launcher','backend') else name+'_dir')
              for name in ('launcher','backend','web','resource','redis','media','licenses','sources')}
    records = {}
    for name, path in inputs.items():
        if not path.exists():
            raise ValueError('missing input: ' + name)
        files = inventory(path)
        if not files:
            raise ValueError('empty input: ' + name)
        records[name] = {'files': files, 'sha256': hashlib.sha256(json.dumps(files, sort_keys=True).encode()).hexdigest()}
        if path.is_dir() and args.output_dir.resolve().is_relative_to(path.resolve()):
            raise ValueError('output overlaps input: ' + name)
    for name, relative in [('web','index.html'), ('redis','redis-server.exe'), ('media','MediaServer.exe')]:
        if not (inputs[name]/relative).is_file():
            raise ValueError('missing required ' + name + ' file')
    if not any(re.search(r'license|licence|copying|copyright', x['path'], re.I) for x in records['licenses']['files']):
        raise ValueError('license materials required')
    root = args.output_dir
    release = root/'releases'/args.version
    release.mkdir(parents=True)
    (release/'backend').mkdir()
    shutil.copy2(args.launcher, root/'UVP.exe')
    shutil.copy2(args.backend, release/'backend/uvp-server.exe')
    for name in ('web','resource','redis','media'):
        shutil.copytree(inputs[name], release/name)
    for name in ('licenses','sources'):
        shutil.copytree(inputs[name], root/name)
    write_json(release/'manifest.json', {'format_version':1, 'version':args.version,
               'source_commit':args.source_commit, 'schema_min':1, 'schema_max':3,
               'files':inventory(release)})
    write_json(root/'current.json', {'version':args.version})
    write_json(root/'version.json', {'name':'uvp-gb28181', 'version':args.version})
    write_json(root/'provenance.json', {'source_metadata':{
        'uvp':{'revision':args.source_commit}, 'zlm':{'revision':args.zlm_revision},
        'redis':{'revision':args.redis_revision}}, 'inputs':records,
        'verification':{'git_verified':False,'automated_legal_audit':False,'license_owner':args.license_owner}})
    (root/'stop.cmd').write_bytes(b'@echo off\r\n"%~dp0UVP.exe" --stop\r\npause\r\n')
    args.output_zip.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(args.output_zip, 'x', zipfile.ZIP_DEFLATED) as archive:
        for path in sorted(root.rglob('*')):
            if path.is_file():
                archive.write(path, path.relative_to(root).as_posix())
    print(json.dumps({'zip':str(args.output_zip), 'sha256':digest(args.output_zip)}))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
