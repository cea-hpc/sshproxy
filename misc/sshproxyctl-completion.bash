#!/usr/bin/env bash

_sshproxyctl() {
        local cur prev commands opts
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
        commands="disable enable error_banner forget help show version"
        opts="-h -c ${commands}"

        case "${prev}" in
            # Main commands
            disable)
                COMPREPLY=( $(compgen -W '-all -host -port' -- "${cur}") )
                ;;
            enable)
                COMPREPLY=( $(compgen -W '-all -host -port' -- "${cur}") )
                ;;
            error_banner)
                COMPREPLY=( $(compgen -W '-expire' -- "${cur}") )
                ;;
            forget)
                COMPREPLY=( $(compgen -W '-all -host -port -service -user error_banner host persist' -- "${cur}") )
                ;;
            help)
                COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
                ;;
            show)
                COMPREPLY=( $(compgen -W '-all -csv -groups -json -source -user config connections error_banner groups hosts users' -- "${cur}") )
                ;;
            # Sub-commands
            config)
                COMPREPLY=( $(compgen -W '-groups -source -user' -- "${cur}") )
                ;;
            connections)
                COMPREPLY=( $(compgen -W '-all -csv -json' -- "${cur}") )
                ;;
            groups)
                COMPREPLY=( $(compgen -W '-all -csv -json' -- "${cur}") )
                ;;
            host)
                COMPREPLY=( $(compgen -W '-all -host -port' -- "${cur}") )
                ;;
            hosts)
                COMPREPLY=( $(compgen -W '-csv -json' -- "${cur}") )
                ;;
            persist)
                COMPREPLY=( $(compgen -W '-host -port -service -user' -- "${cur}") )
                ;;
            users)
                COMPREPLY=( $(compgen -W '-all -csv -json' -- "${cur}") )
                ;;
            # Options
            -all)
                COMPREPLY=( $(compgen -W '-csv -json -port connections groups host users' -- "${cur}") )
                ;;
            -csv)
                COMPREPLY=( $(compgen -W '-all connections groups hosts users' -- "${cur}") )
                ;;
            -groups)
                COMPREPLY=( $(compgen -W '-source -user config' -- "${cur}") )
                ;;
            -host)
                COMPREPLY=( $(compgen -W '-port -service -user host persist' -- "${cur}") )
                ;;
            -json)
                COMPREPLY=( $(compgen -W '-all connections groups hosts users' -- "${cur}") )
                ;;
            -port)
                COMPREPLY=( $(compgen -W '-all -host -service -user host persist' -- "${cur}") )
                ;;
            -service)
                COMPREPLY=( $(compgen -W '-host -port -user persist' -- "${cur}") )
                ;;
            -source)
                COMPREPLY=( $(compgen -W '-groups -user config' -- "${cur}") )
                ;;
            -user)
                COMPREPLY=( $(compgen -W '-groups -host -port -service -source config persist' -- "${cur}") )
                ;;
            -c)
                _filedir
                ;;
            # Default
            *)
                COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
                ;;
        esac

        return 0
}

complete -F _sshproxyctl sshproxyctl
