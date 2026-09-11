"""Read-only P0 source verification; this does not certify a Windows release."""
import argparse
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys

COMPONENTS = {'uvp', 'zlm', 'redis'}


def revision(value):
    if not isinstance(value, str) or not re.fullmatch(r'[0-9a-f]{40}', value):
        raise ValueError('source revision must be a full lowercase commit SHA')
    return value


def git(root, *args):
    result = subprocess.run(['git', '-C', str(root), *args], capture_output=True,
                            env={**os.environ, 'GIT_OPTIONAL_LOCKS': '0'})
    if result.returncode:
        raise ValueError('Git source check failed: ' + args[0])
    return result.stdout.decode('utf-8').rstrip('\n')


def check_repository(root, expected):
    if Path(git(root, 'rev-parse', '--show-toplevel')).resolve() != root.resolve():
        raise ValueError('source path must be a Git repository root')
    if git(root, 'rev-parse', 'HEAD') != expected:
        raise ValueError('source HEAD differs from locked revision')
    # Build inputs must be pristine, including ignored configs/binaries. Build out of tree.
    if git(root, 'status', '--porcelain', '--untracked-files=all', '--ignored=matching'):
        raise ValueError('source contains modified, untracked or ignored files')


def submodules(root, prefix=''):
    found = {}
    for item in git(root, 'ls-tree', '-rz', '--full-tree', 'HEAD').split('\0'):
        if not item:
            continue
        info, path = item.split('\t', 1)
        mode, _, sha = info.split()
        if mode != '160000':
            continue
        child = root / path
        if not child.resolve().is_relative_to(root.resolve()):
            raise ValueError('submodule escapes source root')
        check_repository(child, sha)
        found[prefix + path] = sha
        found.update(submodules(child, prefix + path + '/'))
    return found


def verify(lock, roots):
    if lock.get('schema') != 1 or set(lock.get('components', {})) != COMPONENTS:
        raise ValueError('schema 1 requires exactly uvp, zlm and redis components')
    if set(roots) != COMPONENTS:
        raise ValueError('provide all three source roots')
    verified = {}
    for name in sorted(COMPONENTS):
        component = lock['components'][name]
        sha = revision(component.get('revision'))
        pinned = component.get('submodules')
        if not isinstance(pinned, dict):
            raise ValueError('submodules must be an explicit path-to-SHA map')
        for path, commit in pinned.items():
            if not isinstance(path, str) or not path or '\\' in path or ':' in path:
                raise ValueError('invalid submodule path')
            if PurePosixPath(path).is_absolute() or '..' in path.split('/'):
                raise ValueError('invalid submodule path')
            revision(commit)
        root = Path(roots[name])
        check_repository(root, sha)
        tag = component.get('tag')
        if tag is not None:
            if not isinstance(tag, str) or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]*', tag):
                raise ValueError('invalid tag')
            if git(root, 'rev-parse', '--verify', 'refs/tags/' + tag + '^{commit}') != sha:
                raise ValueError('tag differs from locked commit')
        if submodules(root) != pinned:
            raise ValueError(name + ': submodule list differs from lock')
        verified[name] = {'revision': sha, 'submodules': pinned}
    return verified


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--lock', type=Path, required=True)
    for name in sorted(COMPONENTS):
        parser.add_argument('--' + name, type=Path, required=True)
    args = parser.parse_args()
    try:
        lock = json.loads(args.lock.read_text(encoding='utf-8'))
        sources = verify(lock, {name: getattr(args, name) for name in COMPONENTS})
    except (ValueError, OSError, KeyError, TypeError, AttributeError) as error:
        print('ERROR: ' + str(error), file=sys.stderr)
        return 1
    print(json.dumps({'source_verified': sources, 'windows_runtime_verified': False}, indent=2))
    return 0


if __name__ == '__main__':
    sys.exit(main())
