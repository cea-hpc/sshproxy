#!/usr/bin/env bash

_sshproxyctl() {
        local cur prev commands opts
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
        commands="disable enable error_banner forget get_config help show version"
        opts="-h -c ${commands}"

        case "${prev}" in
            help)
                COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
                ;;
            show)
                COMPREPLY=( $(compgen -W '-all -csv -json connections hosts users groups error_banner' -- "${cur}") )
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
            error_banner)
                COMPREPLY=( $(compgen -W '-expire' -- "${cur}") )
                ;;
            get_config)
                COMPREPLY=( $(compgen -W '-user -groups' -- "${cur}") )
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
