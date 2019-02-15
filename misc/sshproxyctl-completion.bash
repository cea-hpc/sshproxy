#!/usr/bin/env bash

_sshproxyctl() {
        local cur prev opts
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
        commands="enable disable help show version"
        opts="-h -c ${commands}"

        case "${prev}" in
            help)
                COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
                ;;
            show)
                COMPREPLY=( $(compgen -W '-csv -json connections hosts' -- "${cur}") )
                ;;
            -csv)
                COMPREPLY=( $(compgen -W 'connections hosts' -- "${cur}") )
                ;;
            -c)
                _filedir
                ;;
            *)
                COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
                ;;
        esac

        return 0
}

complete -F _sshproxyctl sshproxyctl
