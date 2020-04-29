#!/usr/bin/env bash

_sshproxyctl() {
        local cur prev commands opts
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
                COMPREPLY=( $(compgen -W '-all -csv -json connections hosts users groups' -- "${cur}") )
                ;;
            connections)
                COMPREPLY=( $(compgen -W '-all -csv -json' -- "${cur}") )
                ;;
            hosts)
                COMPREPLY=( $(compgen -W '-csv -json' -- "${cur}") )
                ;;
            users)
                COMPREPLY=( $(compgen -W '-all -csv -json' -- "${cur}") )
                ;;
            groups)
                COMPREPLY=( $(compgen -W '-all -csv -json' -- "${cur}") )
                ;;
            -all)
                COMPREPLY=( $(compgen -W '-csv -json connections users groups' -- "${cur}") )
                ;;
            -csv)
                COMPREPLY=( $(compgen -W '-all connections hosts users groups' -- "${cur}") )
                ;;
            -json)
                COMPREPLY=( $(compgen -W '-all connections hosts users groups' -- "${cur}") )
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
