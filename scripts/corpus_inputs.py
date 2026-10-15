"""独立生成的确定性判题输入，不抓取第三方隐藏测试数据。"""

import random


def generated_inputs(slug):
    rng = random.Random('oj-corpus-v1:' + slug)
    inputs = []
    for trial in range(8):
        n = 20 if trial < 7 else 1000
        a = [rng.randint(-100, 100) for _ in range(n)]
        positive = [abs(x)+1 for x in a]
        seq = lambda values: ' '.join(map(str, values))+'\n'
        if slug == 'pair-sum-index':
            # 只有末尾两个奇数，目标为奇数，其余为远离目标的大偶数，保证唯一解。
            values = list(range(10000, 10000+2*(n-2), 2)) + [3, 8]
            rng.shuffle(values); data=f'{n} 11\n'+seq(values)
        elif slug in {'maximum-contiguous-sum','longest-increasing-subsequence','inversions'}:
            data=f'{n}\n'+seq(a)
        elif slug in {'container-water','jump-game','stock-profit','house-robber'}:
            values = positive if slug != 'jump-game' else [rng.randrange(4) for _ in range(n)]
            data=f'{n}\n'+seq(values)
        elif slug in {'binary-search','subarray-sum'}:
            values=sorted(a) if slug=='binary-search' else a
            data=f'{n} {rng.choice(a)}\n'+seq(values)
        elif slug in {'daily-temperatures','huffman'}:
            data=f'{n}\n'+seq([rng.randrange(101) for _ in range(n)] if slug=='daily-temperatures' else positive)
        elif slug in {'sliding-maximum','split-array','min-window-sum'}:
            values=positive if slug!='sliding-maximum' else a
            k=rng.randint(1,n) if slug!='min-window-sum' else sum(values)//2
            data=f'{n} {k}\n'+seq(values)
        elif slug in {'static-range-minimum-query','range-sum'}:
            ranges=[sorted(rng.sample(range(1,n+1),2)) for _ in range(20)]
            data=f'{n} 20\n'+seq(a)+''.join(seq(pair) for pair in ranges)
        elif slug=='longest-unique': data=''.join(rng.choice('abcde') for _ in range(n))+'\n'
        elif slug=='valid-parentheses' or slug=='balanced-bracket-sequence':
            data=('([]{})'*(n//6) if trial%2 else ''.join(rng.choice('()[]{}') for _ in range(n)))+'\n'
        elif slug=='word-frequency-order':
            data=' '.join(rng.choice(['apple','apricot','banana','cat','dog','z']) for _ in range(n))+'\n'
        elif slug in {'merge-overlapping-intervals','interval-schedule'}:
            pairs=[(rng.randint(-50,50),rng.randint(51,100)) for _ in range(n)] if trial%2 else [(i*3,i*3+2) for i in range(n)]
            if slug=='interval-schedule': pairs=[(abs(l),r+100) for l,r in pairs]
            data=f'{n}\n'+''.join(seq(pair) for pair in pairs)
        elif slug=='integer-sqrt': data=str(rng.randrange(10**18))+'\n'
        elif slug=='climbing-stairs': data=str(rng.randrange(100000))+'\n'
        elif slug=='coin-change':
            coins=rng.sample(range(1,100),10); data='10 1000\n'+seq(coins)
        elif slug in {'lcs','edit-distance','kmp'}:
            letters='abcdef'; size=100 if slug!='kmp' else n
            x=''.join(rng.choice(letters) for _ in range(size)); y=x[-20:] if trial%2 else ''.join(rng.choice(letters) for _ in range(20))
            data=x+'\n'+y+'\n'
        elif slug in {'grid-paths','islands','grid-shortest-steps'}:
            rows=[[int(rng.random()<0.3) for _ in range(20)] for _ in range(20)]
            if slug!='islands': rows[0][0]=rows[-1][-1]=0
            joiner=' ' if slug=='grid-paths' else ''
            data='20 20\n'+''.join(joiner.join(map(str,row))+'\n' for row in rows)
        elif slug in {'dependency-topological-order','bipartite'}:
            edges=set()
            for _ in range(n*2):
                u,v=sorted(rng.sample(range(1,n+1),2)); edges.add((u,v))
            if trial%2: edges.add((2,1)); edges.add((1,2))
            data=f'{n} {len(edges)}\n'+''.join(seq(e) for e in sorted(edges))
        elif slug in {'nonnegative-single-source-shortest-path','mst','floyd'}:
            size=min(n,50); edges=[(i,i+1,rng.randrange(20)) for i in range(1,size)]
            edges += [(rng.randrange(1,size+1),rng.randrange(1,size+1),rng.randrange(100)) for _ in range(size*3)]
            head=f'{size} {len(edges)}'
            if slug=='nonnegative-single-source-shortest-path': head+=f' 1 {size}'
            elif slug=='floyd': head+=' 3'
            data=head+'\n'+''.join(seq(e) for e in edges)
            if slug=='floyd': data+=f'1 {size}\n{size} 1\n1 1\n'
        elif slug=='dynamic-connectivity-queries':
            ops=[f"{'U' if rng.randrange(2) else 'Q'} {rng.randrange(1,n+1)} {rng.randrange(1,n+1)}\n" for _ in range(n*2)]
            data=f'{n} {len(ops)}\n'+''.join(ops)
        elif slug in {'tree-depth','tree-diameter','lca'}:
            edges=[(i,rng.randrange(1,i)) for i in range(2,n+1)]
            data=f'{n}'+(' 20' if slug=='lca' else '')+'\n'+''.join(seq(e) for e in edges)
            if slug=='lca': data+=''.join(seq((rng.randrange(1,n+1),rng.randrange(1,n+1))) for _ in range(20))
        elif slug=='reverse-list': data=f'{n}\n'+seq(a)
        elif slug=='single-number':
            values=list(range(n)); values=values+values+[10001]; rng.shuffle(values); data=f'{len(values)}\n'+seq(values)
        elif slug=='zero-one-capacity-pack':
            data='30 200\n'+''.join(seq((rng.randrange(1,100),rng.randrange(1000))) for _ in range(30))
        elif slug=='triangle-path':
            data='30\n'+''.join(seq([rng.randrange(-100,100) for _ in range(i)]) for i in range(1,31))
        elif slug=='n-queens': data=str(trial+3)+'\n'
        elif slug=='mod-power': data=f'{rng.randrange(10**18)} {rng.randrange(10**18)} {rng.randrange(1,10**9)}\n'
        elif slug=='prime-count': data=str(rng.randrange(1,2000000))+'\n'
        elif slug in {'fenwick','segment-tree'}:
            ops=[]
            for i in range(50):
                l,r=sorted(rng.sample(range(1,n+1),2))
                if i%2: ops.append(f'2 {l} {r}\n')
                elif slug=='fenwick': ops.append(f'1 {l} {rng.randrange(-100,100)}\n')
                else: ops.append(f'1 {l} {r} {rng.randrange(-100,100)}\n')
            data=f'{n} 50\n'+seq(a)+''.join(ops)
        elif slug=='trie':
            words=[''.join(rng.choice('abcdef') for _ in range(rng.randrange(1,30))) for _ in range(n)]
            queries=[w[:rng.randrange(1,len(w)+1)] for w in rng.sample(words,20)]
            data=f'{n} 20\n'+'\n'.join(words+queries)+'\n'
        else: raise ValueError('Missing input strategy: '+slug)
        inputs.append(data)
    return inputs
