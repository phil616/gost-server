#!/usr/bin/env python3
"""Exercise the CNB SSH bootstrap locally with fake Docker/baoctl commands."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

import yaml


FAKE_COMMAND = r'''#!/usr/bin/env python3
import json
import os
from pathlib import Path
import sys

command = Path(sys.argv[0]).name
arguments = sys.argv[1:]
with open(os.environ['COMMAND_TRACE'], 'a') as trace:
    trace.write(json.dumps([command, *arguments]) + '\n')
if command == 'baoctl':
    assert arguments == ['sync', 'altasci-tunnel']
    if os.environ.get('FAIL_SYNC') == '1':
        sys.exit(44)
    Path(os.environ['GENERATED_ENV']).write_bytes(Path(os.environ['SOURCE_ENV']).read_bytes())
    sys.exit(0)
action = arguments[0]
if action == 'login':
    assert arguments[1:] == ['--username', 'existing-deploy-user', '--password-stdin', 'registry.example']
    Path(os.environ['TOKEN_CAPTURE']).write_bytes(sys.stdin.buffer.read())
    sys.exit(42 if os.environ.get('FAIL_LOGIN') == '1' else 0)
if action == 'pull':
    assert Path(os.environ['TOKEN_CAPTURE']).exists(), 'pull attempted without registry login'
    sys.exit(43 if os.environ.get('FAIL_PULL') == '1' else 0)
if action == 'create':
    print('fixture-source-container')
elif action == 'cp':
    destination = Path(arguments[-1])
    destination.joinpath('deploy-docker.sh').write_text(
        '#!/usr/bin/env bash\nset -eu\nprintf "%s\\n" "$@" > "$DEPLOY_CAPTURE"\n'
    )
elif action != 'rm':
    raise AssertionError('Unexpected Docker operation: ' + action)
'''


def main():
    root = Path(__file__).resolve().parent.parent
    configuration = yaml.safe_load((root / '.cnb.yml').read_text())
    stage = configuration['main']['push'][0]['stages'][-1]
    assert stage['name'] == '通过 ssh 部署后端'

    with tempfile.TemporaryDirectory(prefix='altasci-cnb-auth-test-') as directory:
        temp = Path(directory)
        marker = temp / 'must-not-exist'
        # Docker env-file values must preserve shell metacharacters as data.
        token = 'test-\'"-$literal-`uname`-$(touch ' + str(marker) + ')=value#literal'
        source_env = temp / 'bao-source.env'
        generated_env = temp / 'generated.env'
        variables = {
            'CNB_TOKEN_USER_NAME': 'existing-deploy-user',
            'CNB_DOCKER_REGISTRY': 'registry.example',
            'CNB_REPO_SLUG_LOWERCASE': 'org/project',
            'CNB_COMMIT': 'a' * 40,
        }

        def replace(match):
            return variables[match.group(1)]

        script = re.sub(r'(?<!\\)\$\{([A-Z_]+)\}', replace, stage['settings']['script'])
        script = script.replace('\\$', '$')
        script = script.replace('/run/baoctl/altasci-tunnel.env', str(generated_env))
        subprocess.run(['sh', '-n'], input=script, text=True, check=True)
        for name in ('docker', 'baoctl'):
            command = temp / name
            command.write_text(FAKE_COMMAND)
            command.chmod(0o700)

        cases = (
            ('success', None, 'DEPLOY_TOKEN=' + token + '\n'),
            ('CRLF and duplicate key', None, 'DEPLOY_TOKEN=old\r\nDEPLOY_TOKEN=' + token + '\r\n'),
            ('no final newline', None, 'DEPLOY_TOKEN=' + token),
            ('sync failure', 'FAIL_SYNC', 'DEPLOY_TOKEN=' + token + '\n'),
            ('login failure', 'FAIL_LOGIN', 'DEPLOY_TOKEN=' + token + '\n'),
            ('pull failure', 'FAIL_PULL', 'DEPLOY_TOKEN=' + token + '\n'),
            ('missing token', 'MISSING_TOKEN', 'OTHER_KEY=value\n'),
            ('empty token', 'EMPTY_TOKEN', 'DEPLOY_TOKEN=\n'),
        )
        for label, failure, contents in cases:
            trace = temp / 'trace.jsonl'
            capture = temp / 'token'
            deployment = temp / 'deployment'
            for path in (trace, capture, deployment, generated_env):
                path.unlink(missing_ok=True)
            source_env.write_text('UNRELATED=$(touch ' + str(marker) + ')\n' + contents, newline='')
            environment = dict(
                os.environ,
                PATH=str(temp) + os.pathsep + os.environ['PATH'],
                COMMAND_TRACE=str(trace), TOKEN_CAPTURE=str(capture), DEPLOY_CAPTURE=str(deployment),
                SOURCE_ENV=str(source_env), GENERATED_ENV=str(generated_env),
                DEPLOY_TOKEN='stale-process-token', FAIL_LOGIN='0', FAIL_PULL='0', FAIL_SYNC='0',
            )
            if failure:
                environment[failure] = '1'
            result = subprocess.run(['sh'], input=script, text=True, capture_output=True,
                                    env=environment, timeout=15)
            events = [json.loads(line) for line in trace.read_text().splitlines()]
            assert events[0] == ['baoctl', 'sync', 'altasci-tunnel']
            assert not marker.exists(), 'Registry token was executed as shell code'
            assert token not in result.stdout + result.stderr
            if failure:
                assert result.returncode != 0
                if failure == 'FAIL_LOGIN':
                    expected = [['baoctl', 'sync'], ['docker', 'login']]
                elif failure == 'FAIL_PULL':
                    expected = [['baoctl', 'sync'], ['docker', 'login'], ['docker', 'pull']]
                else:
                    expected = [['baoctl', 'sync']]
                assert [event[:2] for event in events] == expected
                assert not deployment.exists()
            else:
                assert result.returncode == 0, result.stderr
                assert events[1][:2] == ['docker', 'login']
                assert events[2][:2] == ['docker', 'pull']
                image = 'registry.example/org/project:' + 'a' * 40
                assert deployment.read_text().splitlines() == [
                    image, str(generated_env), 'altasci-server-data', 'bridge',
                    image + '-init-client',
                ]
            if capture.exists():
                assert capture.read_text() == token + '\n'
            print('PASS: ' + label)


if __name__ == '__main__':
    main()
