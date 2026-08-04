import { execFileSync } from 'node:child_process'
import { readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const codegen = dirname(fileURLToPath(import.meta.url))
const spore = resolve(codegen, '..')
const spec = resolve(spore, 'openapi.json')
const cli = resolve(codegen, 'node_modules/.bin/openapi-generator-cli')

const clients = [
  ['python', 'python'],
  ['php', 'php'],
  ['ruby', 'ruby'],
  ['rust', 'rust'],
]

const generatedScaffolding = {
  python: ['.github', '.gitlab-ci.yml', '.travis.yml', 'git_push.sh'],
  php: ['.travis.yml', 'git_push.sh'],
  ruby: ['.gitlab-ci.yml', '.travis.yml', 'git_push.sh'],
  rust: ['.travis.yml', 'git_push.sh'],
}

function normalizeGeneratedFiles(directory) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isSymbolicLink() || ['dist', 'node_modules', 'target'].includes(entry.name)) {
      continue
    }
    if (entry.isDirectory()) {
      normalizeGeneratedFiles(path)
      continue
    }

    const source = readFileSync(path, 'utf8')
    const normalized = source.replace(/[\t ]+$/gm, '').replace(/\n+$/, '\n')
    if (normalized !== source) writeFileSync(path, normalized)
  }
}

normalizeGeneratedFiles(resolve(spore, 'sdk-node'))

for (const [name, generator] of clients) {
  const output = resolve(spore, `sdk-${name}`)
  if (!output.startsWith(`${spore}/sdk-`)) {
    throw new Error(`refusing to replace unexpected SDK path: ${output}`)
  }

  rmSync(output, { recursive: true, force: true })
  const args = [
    'generate',
    '-g',
    generator,
    '-i',
    spec,
    '-o',
    output,
    '-c',
    resolve(codegen, 'config', `${name}.json`),
    '--global-property',
    'apiDocs=false,modelDocs=false,apiTests=false,modelTests=false',
  ]
  if (name === 'python') {
    args.push('--git-user-id', 'lalternativefabrique', '--git-repo-id', 'packages')
  }
  execFileSync(cli, args, { cwd: codegen, stdio: 'inherit' })
  for (const relativePath of generatedScaffolding[name]) {
    rmSync(resolve(output, relativePath), { recursive: true, force: true })
  }
  normalizeGeneratedFiles(output)
}
