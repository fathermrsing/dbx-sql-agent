#!/usr/bin/env python3
"""Validate development DBXP integrity, not signatures or host compatibility."""
import hashlib,json,pathlib,sys,zipfile
for arg in sys.argv[1:]:
    with zipfile.ZipFile(arg) as z:
        names=z.namelist()
        assert len(names)==len(set(names)), 'duplicate entries'
        for name in names:
            p=pathlib.PurePosixPath(name)
            assert not p.is_absolute() and '..' not in p.parts and '\\' not in name, 'unsafe path'
            assert not ((z.getinfo(name).external_attr>>16)&0o170000)==0o120000, 'symlink'
        checks=json.loads(z.read('checksums.json'))
        assert checks['algorithm']=='sha256'
        expected=set(names)-{'checksums.json','signature.json'}
        assert expected==set(checks['files']), 'checksum coverage mismatch'
        for name,digest in checks['files'].items():
            assert hashlib.sha256(z.read(name)).hexdigest()==digest, 'hash mismatch: '+name
        manifest=json.loads(z.read('manifest.json'))
        exe=manifest['entrypoints']['backend']['executable']
        assert exe in expected
        assert (z.getinfo(exe).external_attr>>16)&0o111, 'missing executable bits'
        assert manifest['entrypoints']['ui']['entry'] in expected
        assert 'signature.json' not in names, 'this script expects unsigned development candidates'
        print(f'OK: {arg}: {len(expected)} hashed files; unsigned; installation NOT verified')
